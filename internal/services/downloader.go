package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"fingerprint-converter/internal/pool"
)

// Downloader handles file downloads from URLs (S3, HTTP, HTTPS)
type Downloader struct {
	client     *http.Client
	bufferPool *pool.BufferPool
	maxSize    int64
}

// NewDownloader creates a new downloader with optimized HTTP client
func NewDownloader(bufferPool *pool.BufferPool, maxSize int64, timeout time.Duration) *Downloader {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	if maxSize <= 0 {
		maxSize = 500 * 1024 * 1024 // 500MB default
	}

	// Optimized HTTP client for high throughput
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
			ForceAttemptHTTP2:   true,
		},
	}

	return &Downloader{
		client:     client,
		bufferPool: bufferPool,
		maxSize:    maxSize,
	}
}

// Download fetches a file from URL (S3, HTTP, HTTPS)
func (d *Downloader) Download(ctx context.Context, url string) ([]byte, error) {
	// Validate URL
	if url == "" {
		return nil, fmt.Errorf("empty URL")
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("invalid URL scheme: must be http:// or https://")
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Check content length
	if resp.ContentLength > d.maxSize {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", resp.ContentLength, d.maxSize)
	}

	// Use buffer pool for efficient memory management
	var data []byte
	if resp.ContentLength > 0 {
		// Known size - allocate exact buffer
		if resp.ContentLength <= int64(d.bufferPool.GetStats().Allocated) {
			buf := d.bufferPool.GetSized(int(resp.ContentLength))
			defer d.bufferPool.PutSized(buf)

			n, err := io.ReadFull(resp.Body, buf)
			if err != nil && err != io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("read failed: %w", err)
			}
			data = make([]byte, n)
			copy(data, buf[:n])
		} else {
			// Too large for pool, read directly
			data, err = io.ReadAll(io.LimitReader(resp.Body, d.maxSize))
			if err != nil {
				return nil, fmt.Errorf("read failed: %w", err)
			}
		}
	} else {
		// Unknown size - use limited reader
		data, err = io.ReadAll(io.LimitReader(resp.Body, d.maxSize))
		if err != nil {
			return nil, fmt.Errorf("read failed: %w", err)
		}
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("downloaded file is empty")
	}

	return data, nil
}

// DownloadToFile downloads directly to a file (for large files)
func (d *Downloader) DownloadToFile(ctx context.Context, url, destPath string) error {
	// TODO: Implement streaming download to file for very large files
	// This can be used when file size exceeds memory constraints
	return fmt.Errorf("not implemented yet")
}
