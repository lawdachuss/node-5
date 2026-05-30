package uploader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MixdropUploader handles uploading files to Mixdrop
type MixdropUploader struct {
	email  string
	token  string
	client *http.Client
}

// NewMixdropUploader creates a new Mixdrop uploader instance
func NewMixdropUploader(email, token string) *MixdropUploader {
	return &MixdropUploader{
		email: email,
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

type mixdropUploadResp struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Result  struct {
		Fileref string `json:"fileref"`
		Title   string `json:"title"`
		Name    string `json:"name"`
	} `json:"result"`
}

// Upload uploads a file to Mixdrop and returns the embed link
func (u *MixdropUploader) Upload(filePath string) (string, error) {
	// Credentials go in form fields only — no Authorization header.
	// Build multipart body with exact Content-Length; Mixdrop's nginx proxy
	// rejects chunked transfer encoding with a 400.
	fields := map[string]string{
		"email": u.email,
		"token": u.token,
	}
	body, contentLen, contentType, closer, err := multipartStream(fields, "file", filePath)
	if err != nil {
		return "", fmt.Errorf("build multipart: %w", err)
	}
	defer closer.Close()

	req, err := http.NewRequest("POST", "https://ul.mixdrop.ag/api", body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.ContentLength = contentLen
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload status %d: %s", resp.StatusCode, string(b))
	}

	var uploadResp mixdropUploadResp
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if !uploadResp.Success {
		return "", fmt.Errorf("upload failed: %s", uploadResp.Error)
	}
	if uploadResp.Result.Fileref == "" {
		return "", fmt.Errorf("empty fileref in upload response")
	}

	return fmt.Sprintf("https://mixdrop.ag/e/%s", uploadResp.Result.Fileref), nil
}
