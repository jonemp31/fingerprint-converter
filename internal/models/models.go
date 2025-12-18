package models

// ConvertRequest represents a media conversion request
type ConvertRequest struct {
	DeviceID         string `json:"device_id" validate:"required"`         // Device identifier for caching
	URL              string `json:"url" validate:"required"`               // S3/HTTP URL or base64 data
	MediaType        string `json:"media_type" validate:"required"`        // audio/image/video
	AntiFingerprintLevel string `json:"anti_fingerprint_level"`        // none/basic/moderate/paranoid
	IsBase64         bool   `json:"is_base64"`                            // If true, URL is base64 encoded data
}

// ConvertResponse represents the conversion response
type ConvertResponse struct {
	Success       bool   `json:"success"`
	ProcessedPath string `json:"processed_path"`              // Local path to processed file
	ProcessedURL  string `json:"processed_url,omitempty"`     // S3 URL if uploaded
	CacheHit      bool   `json:"cache_hit"`                   // Whether result came from cache
	MediaType     string `json:"media_type"`                  // audio/image/video
	OriginalSize  int64  `json:"original_size_bytes"`         // Original file size
	ProcessedSize int64  `json:"processed_size_bytes"`        // Processed file size
	SizeIncrease  string `json:"size_increase_percent"`       // Percentage increase
	ProcessingTime string `json:"processing_time_ms"`         // Time taken to process
	CacheExpires  string `json:"cache_expires,omitempty"`     // When cache becomes invalid
	FileExpires   string `json:"file_expires,omitempty"`      // When file will be deleted
}

// CacheStatsResponse represents cache statistics
type CacheStatsResponse struct {
	DeviceID     string                 `json:"device_id,omitempty"`
	GlobalStats  map[string]interface{} `json:"global_stats,omitempty"`
	DeviceStats  map[string]interface{} `json:"device_stats,omitempty"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status        string                 `json:"status"`
	Timestamp     string                 `json:"timestamp"`
	FFmpegVersion string                 `json:"ffmpeg_version"`
	WorkerPool    map[string]interface{} `json:"worker_pool"`
	BufferPool    map[string]interface{} `json:"buffer_pool"`
	Cache         map[string]interface{} `json:"cache"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}
