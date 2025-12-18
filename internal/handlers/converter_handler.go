package handlers

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"fingerprint-converter/internal/cache"
	"fingerprint-converter/internal/models"
	"fingerprint-converter/internal/pool"
	"fingerprint-converter/internal/services"
)

// ConverterHandler handles media conversion requests with caching
type ConverterHandler struct {
	audioConverter *services.AudioConverter
	imageConverter *services.ImageConverter
	videoConverter *services.VideoConverter
	downloader     *services.Downloader
	cache          *cache.DeviceCache
	workerPool     *pool.WorkerPool
	bufferPool     *pool.BufferPool
	requestTimeout time.Duration
	cacheDir       string
}

// NewConverterHandler creates a new converter handler
func NewConverterHandler(
	audioConverter *services.AudioConverter,
	imageConverter *services.ImageConverter,
	videoConverter *services.VideoConverter,
	downloader *services.Downloader,
	deviceCache *cache.DeviceCache,
	workerPool *pool.WorkerPool,
	bufferPool *pool.BufferPool,
	requestTimeout time.Duration,
	cacheDir string,
) *ConverterHandler {
	if requestTimeout <= 0 {
		requestTimeout = 5 * time.Minute
	}

	return &ConverterHandler{
		audioConverter: audioConverter,
		imageConverter: imageConverter,
		videoConverter: videoConverter,
		downloader:     downloader,
		cache:          deviceCache,
		workerPool:     workerPool,
		bufferPool:     bufferPool,
		requestTimeout: requestTimeout,
		cacheDir:       cacheDir,
	}
}

// Convert handles POST /api/convert
func (h *ConverterHandler) Convert(c fiber.Ctx) error {
	start := time.Now()

	// Parse request
	var req models.ConvertRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "Invalid request body",
			Details: err.Error(),
		})
	}

	// Check if download mode is enabled (query param ?download=true)
	downloadMode := c.Query("download") == "true"

	// Validate required fields
	if req.DeviceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "device_id is required",
		})
	}

	if req.URL == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "url is required",
		})
	}

	// Auto-detect media type if not provided
	if req.MediaType == "" {
		req.MediaType = detectMediaType(req.URL)
		if req.MediaType == "" {
			return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
				Success: false,
				Error:   "Could not detect media type from URL. Please provide media_type (audio/image/video)",
				Details: "Supported extensions: audio (.mp3,.opus,.ogg,.m4a,.wav,.aac), image (.jpg,.jpeg,.png,.webp,.gif), video (.mp4,.avi,.mov,.mkv,.webm,.flv)",
			})
		}
		log.Printf("🔍 Auto-detected media type: %s from URL: %s", req.MediaType, truncateURL(req.URL))
	}

	// Set default anti-fingerprint level if not provided
	if req.AntiFingerprintLevel == "" {
		req.AntiFingerprintLevel = getDefaultAFLevel(req.MediaType)
		log.Printf("🎯 Using default AF level: %s for media type: %s", req.AntiFingerprintLevel, req.MediaType)
	}

	// Check cache first
	urlHash := hashURL(req.URL)
	if cachedEntry := h.cache.Get(req.DeviceID, req.URL); cachedEntry != nil {
		// Cache hit - return cached file
		fileInfo, err := os.Stat(cachedEntry.ProcessedPath)
		if err == nil {
			log.Printf("✅ CACHE HIT: device=%s, url=%s, path=%s",
				req.DeviceID, truncateURL(req.URL), cachedEntry.ProcessedPath)

			// If download mode, return file stream
			if downloadMode {
				return h.sendFile(c, cachedEntry.ProcessedPath, cachedEntry.MediaType)
			}

			// Otherwise return JSON
			return c.JSON(models.ConvertResponse{
				Success:        true,
				ProcessedPath:  cachedEntry.ProcessedPath,
				CacheHit:       true,
				MediaType:      cachedEntry.MediaType,
				ProcessedSize:  fileInfo.Size(),
				CacheExpires:   cachedEntry.CacheExpires.Format(time.RFC3339),
				FileExpires:    cachedEntry.FileExpires.Format(time.RFC3339),
				ProcessingTime: fmt.Sprintf("%d", time.Since(start).Milliseconds()),
			})
		}
		// File was deleted, cache entry will be cleaned up
	}

	// Cache miss - process file
	log.Printf("⚡ CACHE MISS: device=%s, url=%s, processing...",
		req.DeviceID, truncateURL(req.URL))

	ctx, cancel := context.WithTimeout(context.Background(), h.requestTimeout)
	defer cancel()

	// Download or decode input data
	var inputData []byte
	var err error

	if req.IsBase64 {
		// Decode base64 data
		inputData, err = base64.StdEncoding.DecodeString(req.URL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
				Success: false,
				Error:   "Failed to decode base64 data",
				Details: err.Error(),
			})
		}
	} else {
		// Download from URL
		inputData, err = h.downloader.Download(ctx, req.URL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
				Success: false,
				Error:   "Failed to download file",
				Details: err.Error(),
			})
		}
	}

	originalSize := int64(len(inputData))

	// Create media-specific subdirectory
	mediaSubdir := getMediaSubdir(req.MediaType)
	mediaCacheDir := filepath.Join(h.cacheDir, mediaSubdir)

	// Ensure media subdirectory exists
	if err := os.MkdirAll(mediaCacheDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Success: false,
			Error:   "Failed to create media cache directory",
			Details: err.Error(),
		})
	}

	// Generate output path in media-specific subdirectory
	var outputPath string
	switch req.MediaType {
	case "audio":
		outputPath = h.audioConverter.GenerateOutputPath(mediaCacheDir, req.DeviceID, urlHash)
	case "image":
		outputPath = h.imageConverter.GenerateOutputPath(mediaCacheDir, req.DeviceID, urlHash)
	case "video":
		outputPath = h.videoConverter.GenerateOutputPath(mediaCacheDir, req.DeviceID, urlHash)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   fmt.Sprintf("Unsupported media_type: %s", req.MediaType),
			Details: "Supported types: audio, image, video",
		})
	}

	// Process file with appropriate converter
	processingStart := time.Now()
	switch req.MediaType {
	case "audio":
		err = h.audioConverter.Convert(ctx, inputData, req.AntiFingerprintLevel, outputPath)
	case "image":
		err = h.imageConverter.Convert(ctx, inputData, req.AntiFingerprintLevel, outputPath)
	case "video":
		err = h.videoConverter.Convert(ctx, inputData, req.AntiFingerprintLevel, outputPath)
	}

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Success: false,
			Error:   fmt.Sprintf("Conversion failed: %s", req.MediaType),
			Details: err.Error(),
		})
	}

	// Get processed file size
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Success: false,
			Error:   "Failed to stat output file",
			Details: err.Error(),
		})
	}

	processedSize := fileInfo.Size()
	sizeIncrease := float64(processedSize-originalSize) / float64(originalSize) * 100

	// Store in cache
	if err := h.cache.Set(req.DeviceID, req.URL, outputPath, req.MediaType, processedSize); err != nil {
		log.Printf("⚠️  Failed to cache file: %v", err)
	}

	// Get cache entry for expiration times
	cacheEntry := h.cache.Get(req.DeviceID, req.URL)
	cacheExpires := ""
	fileExpires := ""
	if cacheEntry != nil {
		cacheExpires = cacheEntry.CacheExpires.Format(time.RFC3339)
		fileExpires = cacheEntry.FileExpires.Format(time.RFC3339)
	}

	log.Printf("✅ PROCESSED: device=%s, type=%s, level=%s, size=%d→%d (+%.1f%%), time=%dms",
		req.DeviceID, req.MediaType, req.AntiFingerprintLevel,
		originalSize, processedSize, sizeIncrease, time.Since(processingStart).Milliseconds())

	// If download mode, return file stream
	if downloadMode {
		return h.sendFile(c, outputPath, req.MediaType)
	}

	// Otherwise return JSON
	return c.JSON(models.ConvertResponse{
		Success:        true,
		ProcessedPath:  outputPath,
		CacheHit:       false,
		MediaType:      req.MediaType,
		OriginalSize:   originalSize,
		ProcessedSize:  processedSize,
		SizeIncrease:   fmt.Sprintf("%.2f%%", sizeIncrease),
		ProcessingTime: fmt.Sprintf("%d", time.Since(start).Milliseconds()),
		CacheExpires:   cacheExpires,
		FileExpires:    fileExpires,
	})
}

// GetCacheStats handles GET /api/cache/stats/:deviceID
func (h *ConverterHandler) GetCacheStats(c fiber.Ctx) error {
	deviceID := c.Params("deviceID")

	if deviceID == "" {
		// Return global stats
		globalStats := h.cache.GetGlobalStats()
		return c.JSON(models.CacheStatsResponse{
			GlobalStats: globalStats,
		})
	}

	// Return device-specific stats
	deviceStats := h.cache.GetDeviceStats(deviceID)
	globalStats := h.cache.GetGlobalStats()

	return c.JSON(models.CacheStatsResponse{
		DeviceID:    deviceID,
		DeviceStats: deviceStats,
		GlobalStats: globalStats,
	})
}

// Health handles GET /api/health
func (h *ConverterHandler) Health(c fiber.Ctx) error {
	// Check FFmpeg availability
	ffmpegVersion := "unknown"
	if output, err := exec.Command("ffmpeg", "-version").Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 0 {
			ffmpegVersion = strings.TrimSpace(lines[0])
		}
	}

	workerStats := h.workerPool.GetStats()
	bufferStats := h.bufferPool.GetStats()
	cacheStats := h.cache.GetGlobalStats()

	return c.JSON(models.HealthResponse{
		Status:        "healthy",
		Timestamp:     time.Now().Format(time.RFC3339),
		FFmpegVersion: ffmpegVersion,
		WorkerPool: map[string]interface{}{
			"max_workers":    workerStats.MaxWorkers,
			"active_workers": workerStats.ActiveWorkers,
			"total_tasks":    workerStats.TotalTasks,
			"failed_tasks":   workerStats.FailedTasks,
			"avg_exec_time":  workerStats.AvgExecTime.String(),
			"queue_size":     workerStats.QueueSize,
		},
		BufferPool: map[string]interface{}{
			"allocated": bufferStats.Allocated,
			"in_use":    bufferStats.InUse,
			"available": bufferStats.Available,
			"hit_rate":  fmt.Sprintf("%.2f%%", bufferStats.HitRate),
		},
		Cache: cacheStats,
	})
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

// detectMediaType detects media type from URL extension
func detectMediaType(url string) string {
	urlLower := strings.ToLower(url)

	// Audio extensions
	if strings.HasSuffix(urlLower, ".mp3") ||
		strings.HasSuffix(urlLower, ".opus") ||
		strings.HasSuffix(urlLower, ".ogg") ||
		strings.HasSuffix(urlLower, ".m4a") ||
		strings.HasSuffix(urlLower, ".wav") ||
		strings.HasSuffix(urlLower, ".aac") {
		return "audio"
	}

	// Image extensions
	if strings.HasSuffix(urlLower, ".jpg") ||
		strings.HasSuffix(urlLower, ".jpeg") ||
		strings.HasSuffix(urlLower, ".png") ||
		strings.HasSuffix(urlLower, ".webp") ||
		strings.HasSuffix(urlLower, ".gif") {
		return "image"
	}

	// Video extensions
	if strings.HasSuffix(urlLower, ".mp4") ||
		strings.HasSuffix(urlLower, ".avi") ||
		strings.HasSuffix(urlLower, ".mov") ||
		strings.HasSuffix(urlLower, ".mkv") ||
		strings.HasSuffix(urlLower, ".webm") ||
		strings.HasSuffix(urlLower, ".flv") {
		return "video"
	}

	return ""
}

// getDefaultAFLevel returns the recommended AF level for media type
func getDefaultAFLevel(mediaType string) string {
	switch mediaType {
	case "audio":
		return "moderate"
	case "image":
		return "moderate"
	case "video":
		return "basic"
	default:
		return "moderate"
	}
}

// getMediaSubdir returns the subdirectory for the media type
func getMediaSubdir(mediaType string) string {
	switch mediaType {
	case "audio":
		return "audios"
	case "image":
		return "imagens"
	case "video":
		return "videos"
	default:
		return "outros"
	}
}

// sendFile streams file to client with appropriate content type
func (h *ConverterHandler) sendFile(c fiber.Ctx, filePath, mediaType string) error {
	// Set appropriate content type
	var contentType string
	var fileName string

	switch mediaType {
	case "audio":
		contentType = "audio/ogg"
		fileName = filepath.Base(filePath)
	case "image":
		// Detect if JPEG or PNG
		if strings.HasSuffix(filePath, ".jpg") || strings.HasSuffix(filePath, ".jpeg") {
			contentType = "image/jpeg"
		} else {
			contentType = "image/png"
		}
		fileName = filepath.Base(filePath)
	case "video":
		contentType = "video/mp4"
		fileName = filepath.Base(filePath)
	default:
		contentType = "application/octet-stream"
		fileName = filepath.Base(filePath)
	}

	// Set headers
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))

	// Send file
	return c.SendFile(filePath)
}
