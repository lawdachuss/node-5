package uploader

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CatboxUploader handles uploading images to Catbox.moe.
// No API key required — anonymous uploads are supported.
type CatboxUploader struct {
	client *http.Client
}

// NewCatboxUploader creates a new Catbox.moe uploader.
func NewCatboxUploader() *CatboxUploader {
	return &CatboxUploader{
		client: newNoProxyClient(2 * time.Minute),
	}
}

// Upload uploads a file to Catbox.moe and returns the direct file URL.
// Retries up to 3 times with exponential backoff (2s, 4s) on transient errors
// (network failures and 5xx server errors). Client errors (4xx) are fatal.
//
// API: POST https://catbox.moe/user/api.php
// Fields: reqtype=fileupload, fileToUpload=@file (multipart)
// Response on success: plain text URL like "https://files.catbox.moe/abc123.webp"
// Response on error: plain text error message starting with an error description.
func (u *CatboxUploader) Upload(filePath string) (string, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 2s, 4s
			time.Sleep(backoff)
		}

		url, err := u.uploadOnce(filePath)
		if err == nil {
			return url, nil
		}

		lastErr = err

		// Only retry on transient errors: network/IO failures or server errors.
		// Client-side errors (file not found, bad response format) are fatal.
		if isRetryableCatboxError(err) {
			continue
		}

		return "", err
	}

	return "", fmt.Errorf("catbox: all 3 attempts failed, last: %w", lastErr)
}

// uploadOnce performs a single upload attempt without retry logic.
func (u *CatboxUploader) uploadOnce(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("catbox: open file: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// reqtype=fileupload is always required for file uploads
	if err := writer.WriteField("reqtype", "fileupload"); err != nil {
		return "", fmt.Errorf("catbox: write reqtype: %w", err)
	}

	part, err := writer.CreateFormFile("fileToUpload", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("catbox: create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("catbox: copy file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("catbox: close writer: %w", err)
	}

	req, err := http.NewRequest("POST", "https://catbox.moe/user/api.php", &buf)
	if err != nil {
		return "", fmt.Errorf("catbox: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("catbox: send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // 64KB max response
	if err != nil {
		return "", fmt.Errorf("catbox: read response: %w", err)
	}

	text := strings.TrimSpace(string(body))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("catbox: status %d: %s", resp.StatusCode, text)
	}

	// Catbox returns the URL as plain text on success.
	// On error it returns a message like "Missing reqtype" or "No file selected".
	if text == "" {
		return "", fmt.Errorf("catbox: empty response")
	}

	// Catbox returns the URL directly — no JSON wrapping.
	if !strings.HasPrefix(text, "https://") {
		return "", fmt.Errorf("catbox: unexpected response: %s", text)
	}

	return text, nil
}

// isRetryableCatboxError returns true if the error represents a transient
// failure that might succeed on retry (network glitch, server overload,
// or IP-based rate limiting).
func isRetryableCatboxError(err error) bool {
	errStr := err.Error()

	// Server-side HTTP errors (5xx) — retry
	if strings.Contains(errStr, "status 5") {
		return true
	}

	// "Invalid uploader" (HTTP 412) is often a transient IP block or
	// rate-limit from Catbox's abuse prevention. Retry with backoff.
	if strings.Contains(errStr, "Invalid uploader") {
		return true
	}

	// Network/connection errors — retry
	if strings.Contains(errStr, "send request") {
		return true
	}

	// Read errors — retry (could be temporary server reset)
	if strings.Contains(errStr, "read response") {
		return true
	}

	return false
}
