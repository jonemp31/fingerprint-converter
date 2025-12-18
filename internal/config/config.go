package config

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	// Server configuration
	Port         string
	AppEnv       string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	BodyLimit    int

	// Worker pool configuration
	MaxWorkers          int
	QueueSizeMultiplier int
	RequestTimeout      time.Duration

	// Buffer pool configuration
	BufferPoolSize int
	BufferSize     int

	// Cache configuration
	CacheDir     string
	CacheTTL     time.Duration // 28 minutes
	FileTTL      time.Duration // 30 minutes
	EnableCache  bool

	// Performance tuning
	GOGC       int
	GoMemLimit string

	// Download settings
	DownloadTimeout     time.Duration
	MaxDownloadSize     int64

	// Anti-fingerprint settings
	DefaultAFLevel string // none/basic/moderate/paranoid

	// Logging configuration
	LogLevel              string
	EnablePerformanceLogs bool

	// Development settings
	Debug bool

	// Production settings
	ProductionMode bool
	EnableCORS     bool

	// Monitoring settings
	EnableHealthCheck   bool
	EnableStatsEndpoint bool
}

// Load loads configuration from environment variables and .env file
func Load() *Config {
	// Try to load .env file (optional)
	if err := godotenv.Load(); err != nil {
		log.Printf("Note: .env file not found: %v", err)
	} else {
		log.Println("✅ Loaded configuration from .env file")
	}

	return &Config{
		// Server configuration
		Port:         getEnv("PORT", "5001"),
		AppEnv:       getEnv("APP_ENV", "development"),
		ReadTimeout:  getDuration("READ_TIMEOUT", 5*time.Minute),
		WriteTimeout: getDuration("WRITE_TIMEOUT", 5*time.Minute),
		BodyLimit:    getInt("BODY_LIMIT", 500*1024*1024), // 500MB

		// Worker pool - smart defaults based on CPU
		MaxWorkers:          getWorkerCount(),
		QueueSizeMultiplier: getInt("QUEUE_SIZE_MULTIPLIER", 10),
		RequestTimeout:      getDuration("REQUEST_TIMEOUT", 5*time.Minute),

		// Buffer pool - optimized for high throughput
		BufferPoolSize: getInt("BUFFER_POOL_SIZE", 100),
		BufferSize:     getInt("BUFFER_SIZE", 10*1024*1024), // 10MB

		// Cache configuration
		CacheDir:    getEnv("CACHE_DIR", "/tmp/media-cache"),
		CacheTTL:    getDuration("CACHE_TTL", 28*time.Minute),
		FileTTL:     getDuration("FILE_TTL", 30*time.Minute),
		EnableCache: getBool("ENABLE_CACHE", true),

		// GC and memory tuning
		GOGC:       getInt("GOGC", 100),
		GoMemLimit: getEnv("GOMEMLIMIT", "2GiB"),

		// Download settings
		DownloadTimeout: getDuration("DOWNLOAD_TIMEOUT", 30*time.Second),
		MaxDownloadSize: getInt64("MAX_DOWNLOAD_SIZE", 500*1024*1024), // 500MB

		// Anti-fingerprint settings
		DefaultAFLevel: getEnv("DEFAULT_AF_LEVEL", "moderate"),

		// Logging configuration
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		EnablePerformanceLogs: getBool("ENABLE_PERFORMANCE_LOGS", true),

		// Development settings
		Debug: getBool("DEBUG", false),

		// Production settings
		ProductionMode: getBool("PRODUCTION_MODE", false),
		EnableCORS:     getBool("ENABLE_CORS", true),

		// Monitoring settings
		EnableHealthCheck:   getBool("ENABLE_HEALTH_CHECK", true),
		EnableStatsEndpoint: getBool("ENABLE_STATS_ENDPOINT", true),
	}
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
		log.Printf("Warning: Invalid integer value for %s: %s, using default: %d", key, value, defaultValue)
	}
	return defaultValue
}

func getInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
		log.Printf("Warning: Invalid int64 value for %s: %s, using default: %d", key, value, defaultValue)
	}
	return defaultValue
}

func getBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
		log.Printf("Warning: Invalid boolean value for %s: %s, using default: %v", key, value, defaultValue)
	}
	return defaultValue
}

func getDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
		log.Printf("Warning: Invalid duration value for %s: %s, using default: %v", key, value, defaultValue)
	}
	return defaultValue
}

func getWorkerCount() int {
	if value := os.Getenv("MAX_WORKERS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}

	// Default: number of CPU cores * 2
	numCPU := runtime.NumCPU()
	if numCPU < 2 {
		return 4
	}
	return numCPU * 2
}
