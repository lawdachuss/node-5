package uploader

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CatboxUploader handles uploading files to Catbox.moe.
// Anonymous uploads are supported. For more reliable uploads, set the
// CATBOX_USERHASH environment variable (find it on catbox.moe after logging in).
type CatboxUploader struct {
	client   *http.Client
	userhash string
}

// NewCatboxUploader creates a new Catbox.moe uploader.
// Reads CATBOX_USERHASH from the environment for authenticated uploads.
func NewCatboxUploader() *CatboxUploader {
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &CatboxUploader{
		client: &http.Client{
			Timeout:   5 * time.Minute, // 5 min for larger preview MP4s
			Transport: transport,
		},
		userhash: os.Getenv("CATBOX_USERHASH"),
	}
}

// Upload uploads a file to Catbox.moe and returns the direct file URL.
// Retries up to 3 times with exponential backoff (2s, 4s) on transient errors.
//
// API: POST https://catbox.moe/user/api.php
// Fields: reqtype=fileupload, fileToUpload=@file (multipart)
//         userhash=<hash> (optional, for authenticated uploads)
// Response on success: plain text URL like "https://files.catbox.moe/abc123.webp"
// Response on error: plain text error message.
func (u *CatboxUploader) Upload(filePath string) (string, error) {
	var lastErr error

	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 2s, 4s, 8s, 16s
			time.Sleep(backoff)
		}

		url, err := u.uploadOnce(filePath)
		if err == nil {
			return url, nil
		}

		lastErr = err

		if isRetryableCatboxError(err) {
			continue
		}

		return "", err
	}

	return "", fmt.Errorf("catbox: all %d attempts failed, last: %w", 5, lastErr)
}

// mimeTypeFor returns an appropriate Content-Type for a file extension.
// Catbox expects application/octet-stream for most file types.
func mimeTypeFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

// uploadOnce streams the file through io.Pipe with a standard multipart writer.
// This is the canonical Go approach to multipart uploads — it guarantees
// correct boundary formatting and part ordering that Catbox expects.
func (u *CatboxUploader) uploadOnce(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("catbox: open file: %w", err)
	}
	defer file.Close()

	// Use io.Pipe so the multipart writer writes directly into the request body,
	// streaming the file without buffering it in RAM. The multipart writer
	// handles all boundary formatting correctly.
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)

	errChan := make(chan error, 1)
	go func() {
		defer pipeWriter.Close()
		defer writer.Close()

		// reqtype is always required
		if err := writer.WriteField("reqtype", "fileupload"); err != nil {
			errChan <- fmt.Errorf("write reqtype: %w", err)
			return
		}

		// Send userhash if available (authenticated uploads are more reliable)
		if u.userhash != "" {
			if err := writer.WriteField("userhash", u.userhash); err != nil {
				errChan <- fmt.Errorf("write userhash: %w", err)
				return
			}
		}

		// Use CreatePart instead of CreateFormFile so we can set the correct
		// Content-Type for the file (video/mp4 vs application/octet-stream).
		// Catbox may handle files differently based on MIME type.
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="fileToUpload"; filename="%s"`, filepath.Base(filePath)))
		h.Set("Content-Type", mimeTypeFor(filePath))
		part, err := writer.CreatePart(h)
		if err != nil {
			errChan <- fmt.Errorf("create form file: %w", err)
			return
		}

		if _, err := io.Copy(part, file); err != nil {
			errChan <- fmt.Errorf("copy file: %w", err)
			return
		}

		errChan <- nil
	}()

	req, err := http.NewRequest("POST", "https://catbox.moe/user/api.php", pipeReader)
	if err != nil {
		pipeReader.CloseWithError(err)
		return "", fmt.Errorf("catbox: create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://catbox.moe")
	req.Header.Set("Referer", "https://catbox.moe/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Priority", "u=1, i")

	resp, err := u.client.Do(req)
	if err != nil {
		pipeReader.CloseWithError(err)
		// Drain error channel to avoid goroutine leak
		select {
		case <-errChan:
		case <-time.After(5 * time.Second):
		}
		return "", fmt.Errorf("catbox: send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors from the goroutine
	select {
	case err := <-errChan:
		if err != nil {
			return "", err
		}
	case <-time.After(30 * time.Second):
		return "", fmt.Errorf("catbox: timeout waiting for file copy to complete")
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("catbox: read response: %w", err)
	}

	text := strings.TrimSpace(string(raw))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("catbox: status %d: %s", resp.StatusCode, text)
	}

	if text == "" {
		return "", fmt.Errorf("catbox: empty response")
	}

	if !strings.HasPrefix(text, "https://") {
		return "", fmt.Errorf("catbox: unexpected response: %s", text)
	}

	return text, nil
}

// isRetryableCatboxError returns true if the error represents a transient
// failure that might succeed on retry.
func isRetryableCatboxError(err error) bool {
	errStr := err.Error()

	if strings.Contains(errStr, "status 5") {
		return true
	}

	// "Invalid uploader" (HTTP 412) — Catbox may be rate-limiting or
	// blocking the IP. Sleep with backoff and retry.
	if strings.Contains(errStr, "Invalid uploader") {
		return true
	}

	if strings.Contains(errStr, "stat file") || strings.Contains(errStr, "open file") {
		return true
	}

	if strings.Contains(errStr, "send request") || strings.Contains(errStr, "read response") {
		return true
	}

	return false
}
