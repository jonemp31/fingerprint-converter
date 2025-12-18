package cache

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheEntry represents a cached file with metadata
type CacheEntry struct {
	ProcessedPath string    // Path to the processed file
	CacheExpires  time.Time // When cache becomes invalid (28 minutes)
	FileExpires   time.Time // When file should be deleted (30 minutes)
	Created       time.Time // Creation timestamp
	Uses          int64     // Number of cache hits
	Size          int64     // File size in bytes
	MediaType     string    // audio/image/video
	URL           string    // Original URL
}

// DeviceCache manages per-device file caching with fixed TTL
type DeviceCache struct {
	cache         map[string]map[string]*CacheEntry // deviceID -> urlHash -> entry
	mu            sync.RWMutex
	cacheTTL      time.Duration // 28 minutes
	fileTTL       time.Duration // 30 minutes
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	cacheDir      string
	stats         CacheStats
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	Hits          int64
	Misses        int64
	Evictions     int64
	TotalDevices  int
	TotalEntries  int
	TotalSizeKB   int64
	OldestEntry   time.Time
	NewestEntry   time.Time
	mu            sync.RWMutex
}

// NewDeviceCache creates a new device-specific cache manager
func NewDeviceCache(cacheDir string, cacheTTL, fileTTL time.Duration) *DeviceCache {
	if cacheTTL <= 0 {
		cacheTTL = 28 * time.Minute
	}
	if fileTTL <= 0 {
		fileTTL = 30 * time.Minute
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Printf("Warning: Failed to create cache directory %s: %v", cacheDir, err)
	}

	dc := &DeviceCache{
		cache:       make(map[string]map[string]*CacheEntry),
		cacheTTL:    cacheTTL,
		fileTTL:     fileTTL,
		stopCleanup: make(chan struct{}),
		cacheDir:    cacheDir,
	}

	// Start cleanup goroutine (runs every minute)
	dc.cleanupTicker = time.NewTicker(1 * time.Minute)
	go dc.cleanupLoop()

	log.Printf("✅ Device cache initialized: TTL=%v, FileTTL=%v, Dir=%s", cacheTTL, fileTTL, cacheDir)

	return dc
}

// Get retrieves a cached file if still valid
// Returns nil if cache expired or not found
func (dc *DeviceCache) Get(deviceID, url string) *CacheEntry {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	urlHash := hashURL(url)
	deviceCache, exists := dc.cache[deviceID]
	if !exists {
		dc.recordMiss()
		return nil
	}

	entry, exists := deviceCache[urlHash]
	if !exists {
		dc.recordMiss()
		return nil
	}

	// Check if cache expired (28 minutes)
	if time.Now().After(entry.CacheExpires) {
		dc.recordMiss()
		return nil
	}

	// Cache hit - update stats
	entry.Uses++
	dc.recordHit()

	return entry
}

// Set stores a processed file in cache
func (dc *DeviceCache) Set(deviceID, url, processedPath, mediaType string, fileSize int64) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	now := time.Now()
	urlHash := hashURL(url)

	// Initialize device cache if needed
	if dc.cache[deviceID] == nil {
		dc.cache[deviceID] = make(map[string]*CacheEntry)
	}

	entry := &CacheEntry{
		ProcessedPath: processedPath,
		CacheExpires:  now.Add(dc.cacheTTL), // 28 minutes
		FileExpires:   now.Add(dc.fileTTL),  // 30 minutes
		Created:       now,
		Uses:          0,
		Size:          fileSize,
		MediaType:     mediaType,
		URL:           url,
	}

	dc.cache[deviceID][urlHash] = entry

	// Schedule file deletion after fileTTL (30 minutes)
	go dc.scheduleFileDeletion(deviceID, urlHash, processedPath, dc.fileTTL)

	log.Printf("📦 Cache SET: device=%s, url=%s, path=%s, expires=%v",
		deviceID, truncateURL(url), processedPath, entry.CacheExpires.Format("15:04:05"))

	return nil
}

// scheduleFileDeletion deletes the file after the specified TTL
func (dc *DeviceCache) scheduleFileDeletion(deviceID, urlHash, filePath string, ttl time.Duration) {
	time.Sleep(ttl)

	// Remove from cache
	dc.mu.Lock()
	if deviceCache, exists := dc.cache[deviceID]; exists {
		delete(deviceCache, urlHash)
		if len(deviceCache) == 0 {
			delete(dc.cache, deviceID)
		}
	}
	dc.mu.Unlock()

	// Delete physical file
	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("⚠️  Failed to delete expired file %s: %v", filePath, err)
		}
	} else {
		dc.stats.mu.Lock()
		dc.stats.Evictions++
		dc.stats.mu.Unlock()
		log.Printf("🗑️  Deleted expired file: %s (age: %v)", filepath.Base(filePath), ttl)
	}
}

// cleanupLoop runs periodic cleanup to remove expired entries
func (dc *DeviceCache) cleanupLoop() {
	for {
		select {
		case <-dc.cleanupTicker.C:
			dc.cleanup()
		case <-dc.stopCleanup:
			dc.cleanupTicker.Stop()
			return
		}
	}
}

// cleanup removes expired cache entries
func (dc *DeviceCache) cleanup() {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	now := time.Now()
	expiredFiles := []string{}

	for deviceID, deviceCache := range dc.cache {
		for urlHash, entry := range deviceCache {
			// Remove entries where file already expired (30 minutes)
			if now.After(entry.FileExpires) {
				expiredFiles = append(expiredFiles, entry.ProcessedPath)
				delete(deviceCache, urlHash)
			}
		}

		// Remove empty device caches
		if len(deviceCache) == 0 {
			delete(dc.cache, deviceID)
		}
	}

	// Delete physical files outside lock
	if len(expiredFiles) > 0 {
		go func() {
			for _, filePath := range expiredFiles {
				if err := os.Remove(filePath); err != nil {
					if !os.IsNotExist(err) {
						log.Printf("⚠️  Cleanup failed to delete %s: %v", filePath, err)
					}
				}
			}
			log.Printf("🧹 Cleanup: removed %d expired files", len(expiredFiles))
		}()
	}
}

// GetDeviceStats returns cache statistics for a specific device
func (dc *DeviceCache) GetDeviceStats(deviceID string) map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	deviceCache, exists := dc.cache[deviceID]
	if !exists {
		return map[string]interface{}{
			"entries":   0,
			"total_kb":  0,
			"hit_rate":  0.0,
		}
	}

	totalSize := int64(0)
	entryCount := 0
	for _, entry := range deviceCache {
		totalSize += entry.Size
		entryCount++
	}

	return map[string]interface{}{
		"entries":   entryCount,
		"total_kb":  totalSize / 1024,
		"cache_ttl": dc.cacheTTL.Minutes(),
		"file_ttl":  dc.fileTTL.Minutes(),
	}
}

// GetGlobalStats returns overall cache statistics
func (dc *DeviceCache) GetGlobalStats() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	dc.stats.mu.RLock()
	defer dc.stats.mu.RUnlock()

	totalEntries := 0
	totalSize := int64(0)

	for _, deviceCache := range dc.cache {
		totalEntries += len(deviceCache)
		for _, entry := range deviceCache {
			totalSize += entry.Size
		}
	}

	hitRate := 0.0
	if total := dc.stats.Hits + dc.stats.Misses; total > 0 {
		hitRate = float64(dc.stats.Hits) / float64(total) * 100
	}

	return map[string]interface{}{
		"devices":      len(dc.cache),
		"entries":      totalEntries,
		"total_mb":     totalSize / (1024 * 1024),
		"hits":         dc.stats.Hits,
		"misses":       dc.stats.Misses,
		"evictions":    dc.stats.Evictions,
		"hit_rate":     fmt.Sprintf("%.2f%%", hitRate),
		"cache_ttl_min": dc.cacheTTL.Minutes(),
		"file_ttl_min":  dc.fileTTL.Minutes(),
	}
}

// Stop gracefully shuts down the cache
func (dc *DeviceCache) Stop() {
	close(dc.stopCleanup)
	log.Println("🛑 Device cache stopped")
}

// Helper functions

func hashURL(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])
}

func truncateURL(url string) string {
	if len(url) > 60 {
		return url[:57] + "..."
	}
	return url
}

func (dc *DeviceCache) recordHit() {
	dc.stats.mu.Lock()
	dc.stats.Hits++
	dc.stats.mu.Unlock()
}

func (dc *DeviceCache) recordMiss() {
	dc.stats.mu.Lock()
	dc.stats.Misses++
	dc.stats.mu.Unlock()
}
