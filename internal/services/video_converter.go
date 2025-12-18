package services

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fingerprint-converter/internal/pool"
)

// VideoConverter handles video conversion with anti-fingerprinting
type VideoConverter struct {
	workerPool *pool.WorkerPool
	bufferPool *pool.BufferPool
	mu         sync.RWMutex
	stats      VideoStats
}

// VideoStats tracks conversion metrics
type VideoStats struct {
	TotalConversions  int64
	FailedConversions int64
	AvgConversionTime time.Duration
}

// NewVideoConverter creates a new video converter
func NewVideoConverter(workerPool *pool.WorkerPool, bufferPool *pool.BufferPool) *VideoConverter {
	return &VideoConverter{
		workerPool: workerPool,
		bufferPool: bufferPool,
	}
}

// Convert processes video with anti-fingerprinting
func (vc *VideoConverter) Convert(ctx context.Context, inputData []byte, level string, outputPath string) error {
	start := time.Now()

	// Validate input
	if len(inputData) == 0 {
		return fmt.Errorf("empty input data")
	}

	// Get original video bitrate
	originalBitrate, err := vc.getVideoBitrate(ctx, inputData)
	if err != nil {
		// If we can't get bitrate, use a default
		originalBitrate = 2000
	}

	// Get randomized parameters based on level
	params := vc.getRandomizedParams(level, originalBitrate)

	// Build FFmpeg command with anti-fingerprinting
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0", // Input from stdin
	)

	// Video filters for anti-fingerprinting
	videoFilters := []string{}

	// Add subtle noise (basic, moderate, paranoid)
	if params.addNoise {
		videoFilters = append(videoFilters, fmt.Sprintf("noise=alls=%d:allf=t+u", params.noiseStrength))
	}

	// Add color adjustment (moderate, paranoid)
	if params.colorAdjust {
		videoFilters = append(videoFilters, fmt.Sprintf("eq=brightness=%.6f:contrast=%.6f:saturation=%.6f",
			params.brightness, params.contrast, params.saturation))
	}

	// Add timestamp in metadata (paranoid)
	if params.addTimestamp {
		videoFilters = append(videoFilters, fmt.Sprintf("drawtext=text='':x=0:y=0:fontsize=1:fontcolor=black@0.01"))
	}

	if len(videoFilters) > 0 {
		cmd.Args = append(cmd.Args, "-vf", strings.Join(videoFilters, ","))
	}

	// Video codec settings
	cmd.Args = append(cmd.Args,
		"-c:v", "libx264",
		"-b:v", fmt.Sprintf("%dk", params.bitrate),
		"-crf", strconv.Itoa(params.crf),
		"-preset", params.preset,
		"-g", strconv.Itoa(params.keyframeInterval),
		"-bf", "2", // B-frames
		"-movflags", "+faststart", // Optimize for streaming
	)

	// Audio settings (copy or re-encode depending on level)
	if level == "none" || level == "basic" {
		cmd.Args = append(cmd.Args, "-c:a", "copy") // Copy audio stream
	} else {
		// Re-encode audio with slight variations
		cmd.Args = append(cmd.Args,
			"-c:a", "aac",
			"-b:a", fmt.Sprintf("%dk", 128+rand.Intn(16)), // 128-143k
			"-ar", "48000",
		)
	}

	// Output settings
	cmd.Args = append(cmd.Args,
		"-f", "mp4",
		"-threads", "0",
		"pipe:1", // Output to stdout
	)

	// Set up pipes
	cmd.Stdin = bytes.NewReader(inputData)
	var outputBuffer bytes.Buffer
	var errorBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &errorBuffer

	// Execute conversion
	if err := cmd.Run(); err != nil {
		vc.recordFailure()
		return fmt.Errorf("ffmpeg error: %v, stderr: %s", err, errorBuffer.String())
	}

	output := outputBuffer.Bytes()
	if len(output) == 0 {
		vc.recordFailure()
		return fmt.Errorf("ffmpeg produced no output")
	}

	// Write to file
	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		vc.recordFailure()
		return fmt.Errorf("failed to write output file: %w", err)
	}

	vc.recordSuccess(time.Since(start))
	return nil
}

type videoParams struct {
	bitrate           int
	crf               int
	preset            string
	keyframeInterval  int
	addNoise          bool
	noiseStrength     int
	colorAdjust       bool
	brightness        float64
	contrast          float64
	saturation        float64
	addTimestamp      bool
}

func (vc *VideoConverter) getRandomizedParams(level string, originalBitrate int) videoParams {
	params := videoParams{
		bitrate:          originalBitrate,
		crf:              23,
		preset:           "medium",
		keyframeInterval: 250,
	}

	switch level {
	case "basic":
		// Minimal randomization (recommended for video)
		bitrateVariation := int(float64(originalBitrate) * (0.05 + float64(rand.Intn(6))/100.0)) // 5-10%
		params.bitrate = originalBitrate + bitrateVariation - rand.Intn(bitrateVariation*2)
		params.crf = 22 + rand.Intn(3)              // 22-24
		params.keyframeInterval = 240 + rand.Intn(21) // 240-260

	case "moderate":
		// Moderate randomization
		bitrateVariation := int(float64(originalBitrate) * (0.08 + float64(rand.Intn(5))/100.0)) // 8-12%
		params.bitrate = originalBitrate + bitrateVariation - rand.Intn(bitrateVariation*2)
		params.crf = 22 + rand.Intn(4)              // 22-25
		params.keyframeInterval = 230 + rand.Intn(41) // 230-270
		params.addNoise = true
		params.noiseStrength = 1 + rand.Intn(2)     // 1-2
		params.colorAdjust = true
		params.brightness = float64(rand.Intn(3)-1) / 1000.0     // ±0.001
		params.contrast = 1.0 + float64(rand.Intn(3)-1)/1000.0   // ±0.001
		params.saturation = 1.0 + float64(rand.Intn(3)-1)/1000.0 // ±0.001

	case "paranoid":
		// Maximum randomization
		bitrateVariation := int(float64(originalBitrate) * (0.10 + float64(rand.Intn(6))/100.0)) // 10-15%
		params.bitrate = originalBitrate + bitrateVariation - rand.Intn(bitrateVariation*2)
		params.crf = 21 + rand.Intn(5)              // 21-25
		params.keyframeInterval = 220 + rand.Intn(61) // 220-280
		params.preset = []string{"fast", "medium", "medium"}[rand.Intn(3)] // Vary preset
		params.addNoise = true
		params.noiseStrength = 2 + rand.Intn(4)     // 2-5
		params.colorAdjust = true
		params.brightness = float64(rand.Intn(5)-2) / 1000.0     // ±0.002
		params.contrast = 1.0 + float64(rand.Intn(5)-2)/1000.0   // ±0.002
		params.saturation = 1.0 + float64(rand.Intn(5)-2)/1000.0 // ±0.002
		params.addTimestamp = true

	default: // "none"
		params.bitrate = originalBitrate
		params.crf = 23
		params.keyframeInterval = 250
	}

	return params
}

// getVideoBitrate probes the video to get its bitrate
func (vc *VideoConverter) getVideoBitrate(ctx context.Context, inputData []byte) (int, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=bit_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"-i", "pipe:0",
	)

	cmd.Stdin = bytes.NewReader(inputData)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	bitrateStr := strings.TrimSpace(string(output))
	bitrate, err := strconv.Atoi(bitrateStr)
	if err != nil {
		return 0, err
	}

	// Convert to kbps
	return bitrate / 1000, nil
}

func (vc *VideoConverter) recordSuccess(duration time.Duration) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.stats.TotalConversions++
	vc.stats.AvgConversionTime = (vc.stats.AvgConversionTime*time.Duration(vc.stats.TotalConversions-1) + duration) / time.Duration(vc.stats.TotalConversions)
}

func (vc *VideoConverter) recordFailure() {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.stats.FailedConversions++
}

// GetStats returns current statistics
func (vc *VideoConverter) GetStats() VideoStats {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.stats
}

// GetOutputExtension returns the file extension for this converter
func (vc *VideoConverter) GetOutputExtension() string {
	return ".mp4"
}

// GenerateOutputPath creates a unique output path
func (vc *VideoConverter) GenerateOutputPath(cacheDir, deviceID, urlHash string) string {
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("%s_%s_%d%s", deviceID, urlHash[:8], timestamp, vc.GetOutputExtension())
	return filepath.Join(cacheDir, filename)
}
