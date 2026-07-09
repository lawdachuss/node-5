package uploader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const doodStreamAPIBase = "https://doodapi.co/api"

type DoodStreamUploader struct {
	keys         *keyRing
	client       *http.Client
	cachedServer string
}

func NewDoodStreamUploader(apiKeys []string) *DoodStreamUploader {
	return &DoodStreamUploader{
		keys:   newKeyRing(apiKeys),
		client: newDirectClient(120 * time.Minute),
	}
}

type doodStreamServerResponse struct {
	Msg        string `json:"msg"`
	Status     int    `json:"status"`
	Result     string `json:"result"`
	ServerTime string `json:"server_time"`
}

type doodStreamUploadResult struct {
	DownloadURL string `json:"download_url"`
	FileCode    string `json:"filecode"`
	Status      int    `json:"status"`
	Title       string `json:"title"`
}

type doodStreamUploadResponse struct {
	Msg        string                   `json:"msg"`
	Status     int                      `json:"status"`
	Result     []doodStreamUploadResult `json:"result"`
	ServerTime string                   `json:"server_time"`
}

func (u *DoodStreamUploader) Upload(filePath string) (string, error) {
	return u.UploadWithProgress(filePath, nil)
}

func (u *DoodStreamUploader) Keys() *keyRing { return u.keys }

func (u *DoodStreamUploader) UploadWithProgress(filePath string, progress ProgressFunc) (string, error) {
	if u.keys.count() == 0 {
		return "", fmt.Errorf("DoodStream API key not configured")
	}

	attempts := u.keys.count()
	maxRetriesPerKey := 3
	var lastErr error

	for ki := 0; ki < attempts; ki++ {
		key := u.keys.current()
		for retry := 1; retry <= maxRetriesPerKey; retry++ {
			if retry > 1 {
				time.Sleep(uploadBackoff(retry-2, lastErr))
			}

			downloadLink, err := u.uploadFile(filePath, progress, key)
			if err != nil {
				lastErr = fmt.Errorf("upload file: %w", err)
				if IsPermanentError(err) {
					u.cachedServer = ""
					u.keys.rotate()
					break
				}
				if isUploadRateLimited(err) {
					time.Sleep(uploadBackoff(retry, err))
					lastErr = nil
					continue
				}
				if retry < maxRetriesPerKey {
					continue
				}
				u.keys.rotate()
				break
			}

			return downloadLink, nil
		}
	}

	return "", lastErr
}

func (u *DoodStreamUploader) getUploadServer(apiKey string) (string, error) {
	url := fmt.Sprintf("%s/upload/server?key=%s", doodStreamAPIBase, apiKey)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request upload server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("status 429: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var serverResp doodStreamServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&serverResp); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if serverResp.Status != 200 {
		return "", fmt.Errorf("api status %d: %s", serverResp.Status, serverResp.Msg)
	}
	if serverResp.Result == "" {
		return "", fmt.Errorf("no upload server URL in response")
	}

	return serverResp.Result, nil
}

func (u *DoodStreamUploader) uploadFile(filePath string, progress ProgressFunc, apiKey string) (string, error) {
	uploadServer := u.cachedServer
	if uploadServer == "" {
		var err error
		uploadServer, err = u.getUploadServer(apiKey)
		if err != nil {
			return "", fmt.Errorf("get upload server: %w", err)
		}
		u.cachedServer = uploadServer
	}

	uploadURL := uploadServer
	if !strings.Contains(uploadURL, "?") {
		uploadURL = uploadServer + "?" + apiKey
	} else {
		uploadURL = uploadServer + "&" + apiKey
	}

	body, contentLen, contentType, file, err := multipartStreamWithProgress(
		map[string]string{"api_key": apiKey},
		"file", filePath, "DoodStream", progress,
	)
	if err != nil {
		return "", fmt.Errorf("multipart stream: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequest("POST", uploadURL, body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = contentLen

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("status 429: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	rawBody, _ := io.ReadAll(resp.Body)

	var uploadResp doodStreamUploadResponse
	if err := json.Unmarshal(rawBody, &uploadResp); err != nil {
		return "", fmt.Errorf("decode upload response: %w (body: %s)", err, string(rawBody))
	}

	if uploadResp.Status != 200 {
		return "", fmt.Errorf("upload failed: status %d — %s (body: %s)", uploadResp.Status, uploadResp.Msg, string(rawBody))
	}

	if len(uploadResp.Result) == 0 {
		return "", fmt.Errorf("no files in upload response (body: %s)", string(rawBody))
	}

	fileCode := uploadResp.Result[0].FileCode
	if fileCode == "" {
		var fallback struct {
			Result []struct {
				FileCode string `json:"file_code"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rawBody, &fallback); err == nil && len(fallback.Result) > 0 && fallback.Result[0].FileCode != "" {
			fileCode = fallback.Result[0].FileCode
		}
	}
	if fileCode == "" {
		return "", fmt.Errorf("no file code in response (body: %s)", string(rawBody))
	}

	return fmt.Sprintf("https://dood.to/e/%s", fileCode), nil
}