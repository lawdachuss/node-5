package uploader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// PixeldrainUploader handles uploading files to PixelDrain
type PixeldrainUploader struct {
	token  string
	client *http.Client
}

// NewPixeldrainUploader creates a new Pixeldrain uploader instance
func NewPixeldrainUploader(token string) *PixeldrainUploader {
	return &PixeldrainUploader{
		token: token,
		client: &http.Client{
			Timeout: 120 * time.Minute,
			Transport: &http.Transport{
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   2,
				IdleConnTimeout:       90 * time.Second,
				DisableCompression:    true,
				TLSHandshakeTimeout:   30 * time.Second,
				ResponseHeaderTimeout: 120 * time.Second,
				DisableKeepAlives:     true,
			},
		},
	}
}

// Upload uploads a file to PixelDrain using PUT /api/file/{name} with the raw
// file body and an explicit Content-Length. This avoids chunked transfer
// encoding, which PixelDrain's API does not support reliably.
func (u *PixeldrainUploader) Upload(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	name := filepath.Base(filePath)
	url := fmt.Sprintf("https://pixeldrain.com/api/file/%s", name)

	req, err := http.NewRequest("PUT", url, file)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.ContentLength = fi.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", defaultUserAgent)

	// PixelDrain authenticates via HTTP Basic Auth: empty username, token as password.
	if u.token != "" {
		req.SetBasicAuth("", u.token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		ID      string `json:"id"`
		FileID  string `json:"file_id"`
		Success bool   `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if out.ID != "" {
		return fmt.Sprintf("https://pixeldrain.com/u/%s", out.ID), nil
	}
	if out.FileID != "" {
		return fmt.Sprintf("https://pixeldrain.com/u/%s", out.FileID), nil
	}
	return "", fmt.Errorf("no file ID in upload response")
}
