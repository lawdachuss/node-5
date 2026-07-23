package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client represents a Supabase database client
type Client struct {
	URL    string
	APIKey string
	client *http.Client
}

// NewClient creates a new Supabase client
func NewClient(url, apiKey string) *Client {
	return &Client{
		URL:    url,
		APIKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// ============================================================================
// HTTP HELPERS
// ============================================================================

func (c *Client) request(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.URL+"/rest/v1"+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("apikey", c.APIKey)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Prefer", "resolution=merge-duplicates,return=representation")
	}

	return c.client.Do(req)
}

// requestWithRetryCtx executes the request with a context and retries on transient errors:
// - 503 PGRST002 — schema cache rebuilding after migration
// - 400 PGRST204 — column not in schema cache yet (PostgREST needs to refresh)
func (c *Client) requestWithRetryCtx(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check context before each attempt
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled before attempt %d/%d: %w", attempt+1, maxRetries, err)
		}

		resp, err := c.request(ctx, method, path, body)

		// Context deadline exceeded takes priority over transport errors
		if err != nil && ctx.Err() != nil {
			return nil, fmt.Errorf("request aborted by context: %w", ctx.Err())
		}
		if err != nil {
			lastErr = err
			if attempt < maxRetries-1 {
				backoff := retryBackoff(attempt)
				fmt.Printf("[WARN] Supabase request failed (attempt %d/%d), retrying in %v: %v\n", attempt+1, maxRetries, backoff, err)
				time.Sleep(backoff)
				continue
			}
			return nil, err
		}

		// Check for transient errors that need retry
		if resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500 || resp.StatusCode == 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			bodyStr := string(bodyBytes)

			// PGRST002: schema cache rebuilding after migration
			if resp.StatusCode == 503 && strings.Contains(bodyStr, "PGRST002") {
				lastErr = fmt.Errorf("HTTP 503: %s", bodyStr)
				backoff := retryBackoff(attempt)
				fmt.Printf("[WARN] Supabase schema cache rebuilding (attempt %d/%d), retrying in %v\n", attempt+1, maxRetries, backoff)
				resp.Body.Close()
				time.Sleep(backoff)
				continue
			}

			// PGRST204: column not yet in PostgREST schema cache
			if resp.StatusCode == 400 && strings.Contains(bodyStr, "PGRST204") {
				lastErr = fmt.Errorf("HTTP 400: %s", bodyStr)
				backoff := retryBackoff(attempt)
				fmt.Printf("[WARN] Supabase schema cache stale — column missing (attempt %d/%d), retrying in %v\n", attempt+1, maxRetries, backoff)
				resp.Body.Close()
				time.Sleep(backoff)
				continue
			}

			// Non-retryable error — return as-is
			if resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyStr)
				resp.Body.Close()
				if attempt < maxRetries-1 {
					backoff := retryBackoff(attempt)
					fmt.Printf("[WARN] Supabase transient HTTP %d (attempt %d/%d), retrying in %v\n", resp.StatusCode, attempt+1, maxRetries, backoff)
					time.Sleep(backoff)
					continue
				}
				return nil, lastErr
			}

			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyStr)
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// requestWithRetry wraps requestWithRetryCtx with a 45-second total deadline
// so goroutines cannot hang for >194s (3 retries × 60s client timeout + backoff).
func (c *Client) requestWithRetry(method, path string, body interface{}) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return c.requestWithRetryCtx(ctx, method, path, body)
}

func retryBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(1<<attempt) * 2 * time.Second
}

func (c *Client) get(path string, result interface{}) error {
	resp, err := c.requestWithRetry("GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

// Get performs a GET against the Supabase REST API and decodes the JSON
// response into result. Exported so callers outside the package (e.g. CLI
// commands) can run ad-hoc queries.
func (c *Client) Get(path string, result interface{}) error {
	return c.get(path, result)
}

func (c *Client) post(path string, body interface{}, result interface{}) error {
	resp, err := c.requestWithRetry("POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) patch(path string, body interface{}) error {
	resp, err := c.requestWithRetry("PATCH", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func (c *Client) delete(path string) error {
	resp, err := c.requestWithRetry("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// ============================================================================
// CHANNELS
// ============================================================================

type Channel struct {
	ID          string `json:"id,omitempty"`
	Username    string `json:"username"`
	IsPaused    bool   `json:"is_paused"`
	Framerate   int    `json:"framerate"`
	Resolution  int    `json:"resolution"`
	Pattern     string `json:"pattern"`
	MaxDuration int    `json:"max_duration"`
	MaxFilesize int    `json:"max_filesize"`
	Compress    bool   `json:"compress"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// SaveChannel creates or updates a channel using Supabase's upsert functionality.
// Uses on_conflict to atomically upsert by username, avoiding TOCTOU race conditions.
func (c *Client) SaveChannel(ch *Channel) error {
	resp, err := c.requestWithRetry("POST", "/channels?on_conflict=username", ch)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// GetChannel retrieves a channel by username
func (c *Client) GetChannel(username string) (*Channel, error) {
	var channels []Channel
	err := c.get(fmt.Sprintf("/channels?username=eq.%s&limit=1", url.QueryEscape(username)), &channels)
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("channel not found")
	}
	return &channels[0], nil
}

// GetAllChannels retrieves all channels
func (c *Client) GetAllChannels() ([]Channel, error) {
	var channels []Channel
	err := c.get("/channels?order=created_at.desc&limit=50000", &channels)
	return channels, err
}

// DeleteChannel removes a channel
func (c *Client) DeleteChannel(username string) error {
	return c.delete(fmt.Sprintf("/channels?username=eq.%s", url.QueryEscape(username)))
}

// DeleteChannelsNotIn removes all channel rows whose username is NOT in the
// provided list. Pass an empty slice to delete all channels.
func (c *Client) DeleteChannelsNotIn(usernames []string) error {
	if len(usernames) == 0 {
		return c.delete("/channels")
	}
	// Build a PostgREST "not.in.(a,b,c)" filter.
	// PostgREST expects parentheses and commas to be literal in the filter
	// value, so we only escape the individual usernames.
	list := ""
	for i, u := range usernames {
		if i > 0 {
			list += ","
		}
		list += url.QueryEscape(u)
	}
	return c.delete(fmt.Sprintf("/channels?username=not.in.(%s)", list))
}

// ============================================================================
// RECORDINGS
// ============================================================================

type Recording struct {
	ID           string   `json:"id,omitempty"`
	ChannelID    string   `json:"channel_id,omitempty"`
	Username     string   `json:"username"`
	Filename     string   `json:"filename"`
	Timestamp    string   `json:"timestamp"`
	RoomTitle    string   `json:"room_title,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Viewers      int      `json:"viewers"`
	Resolution   string   `json:"resolution,omitempty"`
	Framerate    int      `json:"framerate"`
	Filesize     int64    `json:"filesize"`
	Duration     float64  `json:"duration,omitempty"`
	Gender       string   `json:"gender,omitempty"`
	ThumbnailURL string   `json:"thumbnail_url,omitempty"`
	PreviewURL   string   `json:"preview_url,omitempty"`
	EmbedURL     string   `json:"embed_url,omitempty"`
	InstanceID   string   `json:"instance_id,omitempty"`
	CreatedAt                string   `json:"created_at,omitempty"`
	UpdatedAt                string   `json:"updated_at,omitempty"`
}

// SaveRecording creates or updates a recording using Supabase's upsert functionality.
// Uses on_conflict to atomically upsert by filename, avoiding TOCTOU race conditions.
func (c *Client) SaveRecording(rec *Recording) error {
	resp, err := c.requestWithRetry("POST", "/recordings?on_conflict=filename", rec)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// PatchRecordingMedia updates only the host-sourced thumbnail and preview URLs
// for a recording, matched by filename. Used by the async media-enrichment step
// that re-fetches poster/preview from the upload host after they are generated.
func (c *Client) PatchRecordingMedia(filename, thumbURL, previewURL string) error {
	// Pass the map directly so requestWithRetry marshals it into the JSON body.
	// (Passing an already-marshaled *bytes.Reader would be re-marshaled by
	// requestWithRetry and produce an empty/incorrect body, silently updating
	// zero rows — see request() which calls json.Marshal on the body arg.)
	body := map[string]interface{}{
		"thumbnail_url": thumbURL,
		"preview_url":   previewURL,
	}
	resp, err := c.requestWithRetry("PATCH", "/recordings?filename=eq."+url.QueryEscape(filename), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// GetRecording retrieves a recording by filename
func (c *Client) GetRecording(filename string) (*Recording, error) {
	var recordings []Recording
	err := c.get(fmt.Sprintf("/recordings?filename=eq.%s&limit=1", url.QueryEscape(filename)), &recordings)
	if err != nil {
		return nil, err
	}
	if len(recordings) == 0 {
		return nil, fmt.Errorf("recording not found")
	}
	return &recordings[0], nil
}

// GetRecordingsByUsername retrieves all recordings for a username
func (c *Client) GetRecordingsByUsername(username string) ([]Recording, error) {
	var recordings []Recording
	err := c.get(fmt.Sprintf("/recordings?username=eq.%s&order=timestamp.desc", url.QueryEscape(username)), &recordings)
	return recordings, err
}

// GetAllRecordings retrieves all recordings by paginating through the
// result set using PostgREST offset/limit (max 1000 per page). This is
// necessary because Supabase free tier caps single-query results at 1000.
// GetNewestRecordings returns up to limit recordings ordered newest-first by
// timestamp. Used by the media watcher to scan only recent recordings (where
// host-generated poster/preview URLs are still likely to appear).
func (c *Client) GetNewestRecordings(limit int) ([]Recording, error) {
	var recs []Recording
	path := fmt.Sprintf("/recordings?order=timestamp.desc&limit=%d", limit)
	if err := c.get(path, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

func (c *Client) GetAllRecordings() ([]Recording, error) {
	var all []Recording
	offset := 0
	pageSize := 1000

	for {
		var page []Recording
		path := fmt.Sprintf("/recordings?order=timestamp.desc&limit=%d&offset=%d", pageSize, offset)
		if err := c.get(path, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}
	return all, nil
}

// DeleteRecording removes a recording
func (c *Client) DeleteRecording(filename string) error {
	return c.delete(fmt.Sprintf("/recordings?filename=eq.%s", url.QueryEscape(filename)))
}

// DeleteUploadLinksByRecordingID removes all upload links for a recording.
func (c *Client) DeleteUploadLinksByRecordingID(recordingID string) error {
	return c.delete(fmt.Sprintf("/upload_links?recording_id=eq.%s", url.QueryEscape(recordingID)))
}

// ============================================================================
// UPLOAD LINKS
// ============================================================================

type UploadLink struct {
	ID          string `json:"id,omitempty"`
	RecordingID string `json:"recording_id"`
	Host        string `json:"host"`
	URL         string `json:"url"`
	InstanceID  string `json:"instance_id,omitempty"`
	UploadedAt  string `json:"uploaded_at,omitempty"`
}

// SaveUploadLink creates or updates an upload link.
func (c *Client) SaveUploadLink(link *UploadLink) error {
	return c.SaveUploadLinks([]UploadLink{*link})
}

// SaveUploadLinks batch-saves all upload links using direct PostgREST table upsert.
func (c *Client) SaveUploadLinks(links []UploadLink) error {
	if len(links) == 0 {
		return nil
	}

	type row struct {
		RecordingID string `json:"recording_id"`
		Host        string `json:"host"`
		URL         string `json:"url"`
		InstanceID  string `json:"instance_id"`
	}
	rows := make([]row, 0, len(links))
	for _, l := range links {
		instanceID := l.InstanceID
		if instanceID == "" {
			instanceID = "default"
		}
		rows = append(rows, row{
			RecordingID: l.RecordingID,
			Host:        l.Host,
			URL:         l.URL,
			InstanceID:  instanceID,
		})
	}

	jsonData, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("marshal upload links: %w", err)
	}

	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequest("POST", c.URL+"/rest/v1/upload_links?on_conflict=recording_id,host", bytes.NewReader(jsonData))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("apikey", c.APIKey)
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries-1 {
				time.Sleep(retryBackoff(attempt))
				continue
			}
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
		}
		return nil
	}
	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// GetUploadLinks retrieves all upload links for a recording.
func (c *Client) GetUploadLinks(recordingID string) ([]UploadLink, error) {
	var links []UploadLink
	if err := c.get(fmt.Sprintf("/upload_links?recording_id=eq.%s", url.QueryEscape(recordingID)), &links); err != nil {
		return nil, err
	}
	return links, nil
}

// GetAllUploadLinks retrieves ALL upload links by paginating through the
// result set using PostgREST Range headers (max 1000 per page). This is
// necessary because Supabase free tier caps single-query results at 1000.
func (c *Client) GetAllUploadLinks() ([]UploadLink, error) {
	var allLinks []UploadLink
	offset := 0
	pageSize := 1000

	for {
		var page []UploadLink
		path := fmt.Sprintf("/upload_links?limit=%d&offset=%d", pageSize, offset)
		if err := c.get(path, &page); err != nil {
			return nil, err
		}
		allLinks = append(allLinks, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}
	return allLinks, nil
}

// ============================================================================
// APP SETTINGS
// ============================================================================

type AppSetting struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

// SaveSetting creates or updates an app setting
func (c *Client) SaveSetting(key string, value interface{}) error {
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}

	setting := &AppSetting{
		Key:   key,
		Value: jsonValue,
	}

	// Upsert using Prefer header
	resp, err := c.requestWithRetry("POST", "/app_settings", setting)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetSetting retrieves an app setting
func (c *Client) GetSetting(key string, result interface{}) error {
	var settings []AppSetting
	err := c.get(fmt.Sprintf("/app_settings?key=eq.%s&limit=1", url.QueryEscape(key)), &settings)
	if err != nil {
		return err
	}
	if len(settings) == 0 {
		return fmt.Errorf("setting not found")
	}

	return json.Unmarshal(settings[0].Value, result)
}

// ============================================================================
// TUNNELS
// ============================================================================

type Tunnel struct {
	ID         string `json:"id,omitempty"`
	URL        string `json:"url"`
	RunID      int    `json:"run_id"`
	InstanceID string `json:"instance_id,omitempty"`
	IsActive   bool   `json:"is_active"`
	CreatedAt  string `json:"created_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// SaveTunnel creates a new tunnel
func (c *Client) SaveTunnel(tunnel *Tunnel) error {
	var result []Tunnel
	return c.post("/tunnels", tunnel, &result)
}

// GetActiveTunnel retrieves the most recent active tunnel for the given instance
func (c *Client) GetActiveTunnel(instanceID string) (*Tunnel, error) {
	var tunnels []Tunnel
	err := c.get(fmt.Sprintf("/tunnels?is_active=eq.true&instance_id=eq.%s&order=created_at.desc&limit=1", url.QueryEscape(instanceID)), &tunnels)
	if err != nil {
		return nil, err
	}
	if len(tunnels) == 0 {
		return nil, fmt.Errorf("no active tunnel found")
	}
	return &tunnels[0], nil
}

// DeactivateOldTunnels marks all tunnels as inactive for the given instance
func (c *Client) DeactivateOldTunnels(instanceID string) error {
	return c.patch(fmt.Sprintf("/tunnels?is_active=eq.true&instance_id=eq.%s", url.QueryEscape(instanceID)), map[string]interface{}{
		"is_active": false,
	})
}

// ============================================================================
// DISK USAGE
// ============================================================================

type DiskUsage struct {
	ID          string `json:"id,omitempty"`
	TotalBytes  int64  `json:"total_bytes"`
	UsedBytes   int64  `json:"used_bytes"`
	FreeBytes   int64  `json:"free_bytes"`
	PercentUsed int    `json:"percent_used"`
	RecordedAt  string `json:"recorded_at,omitempty"`
}

// SaveDiskUsage records current disk usage
func (c *Client) SaveDiskUsage(usage *DiskUsage) error {
	var result []DiskUsage
	return c.post("/disk_usage", usage, &result)
}

// GetLatestDiskUsage retrieves the most recent disk usage record
func (c *Client) GetLatestDiskUsage() (*DiskUsage, error) {
	var usages []DiskUsage
	err := c.get("/disk_usage?order=recorded_at.desc&limit=1", &usages)
	if err != nil {
		return nil, err
	}
	if len(usages) == 0 {
		return nil, fmt.Errorf("no disk usage records found")
	}
	return &usages[0], nil
}

// TrimDiskUsageOlderThan deletes disk_usage rows older than the given retention
// window. The table is append-only (one row per monitor tick across all nodes)
// and only the newest row is ever read, so without trimming it grows unbounded
// and bloats the database.
func (c *Client) TrimDiskUsageOlderThan(retention time.Duration) error {
	cutoff := time.Now().Add(-retention).UTC().Format(time.RFC3339)
	return c.delete(fmt.Sprintf("/disk_usage?recorded_at=lt.%s", url.QueryEscape(cutoff)))
}

// ============================================================================
// UPLOAD JOURNAL
// ============================================================================

type UploadJournal struct {
	ID         string `json:"id,omitempty"`
	FileHash   string `json:"file_hash"`
	Filename   string `json:"filename"`
	Host       string `json:"host"`
	Status     string `json:"status"`
	ErrorMsg   string `json:"error_msg,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// SaveJournalEntry creates or updates an upload journal entry.
// Uses on_conflict to upsert by (file_hash, host).
func (c *Client) SaveJournalEntry(entry *UploadJournal) error {
	resp, err := c.requestWithRetry("POST", "/upload_journal?on_conflict=file_hash,host", entry)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// GetJournalByHash retrieves all journal entries for a given file hash.
func (c *Client) GetJournalByHash(fileHash string) ([]UploadJournal, error) {
	var entries []UploadJournal
	err := c.get(fmt.Sprintf("/upload_journal?file_hash=eq.%s&order=host.asc", url.QueryEscape(fileHash)), &entries)
	return entries, err
}

// GetJournalEntriesByStatus retrieves all journal entries with a given status.
func (c *Client) GetJournalEntriesByStatus(status string) ([]UploadJournal, error) {
	var entries []UploadJournal
	err := c.get(fmt.Sprintf("/upload_journal?status=eq.%s&order=created_at.desc", url.QueryEscape(status)), &entries)
	return entries, err
}

// DeleteJournalByHash removes all journal entries for a file hash (e.g. after local file is deleted).
func (c *Client) DeleteJournalByHash(fileHash string) error {
	return c.delete(fmt.Sprintf("/upload_journal?file_hash=eq.%s", url.QueryEscape(fileHash)))
}

// ============================================================================
// PIPELINE STATES
// ============================================================================
//
// Schema defined in migrate.sql (CREATE TABLE pipeline_states).

type PipelineState struct {
	FileHash     string `json:"file_hash"`
	FilePath     string `json:"file_path"`
	Filename     string `json:"filename"`
	Username     string `json:"username"`
	FileSize     int64  `json:"file_size"`
	CurrentStage string `json:"current_stage"`
	Failed       bool   `json:"failed"`
	LastError    string `json:"last_error,omitempty"`
	ThumbURL     string `json:"thumb_url,omitempty"`
	PreviewURL   string `json:"preview_url,omitempty"`
	EmbedURL     string `json:"embed_url,omitempty"`
	LinksJSON    string `json:"links,omitempty"` // JSON-encoded map[string]string
	Retries      int    `json:"retries,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// SavePipelineState upserts a pipeline state by file_hash.
func (c *Client) SavePipelineState(state *PipelineState) error {
	resp, err := c.requestWithRetry("POST", "/pipeline_states?on_conflict=file_hash", state)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// LoadAllPipelineStates retrieves all pipeline states (for crash recovery on restart).
func (c *Client) LoadAllPipelineStates() ([]PipelineState, error) {
	var states []PipelineState
	err := c.get("/pipeline_states?order=created_at.asc", &states)
	return states, err
}

// DeletePipelineState removes a pipeline state by file hash.
func (c *Client) DeletePipelineState(fileHash string) error {
	return c.delete(fmt.Sprintf("/pipeline_states?file_hash=eq.%s", url.QueryEscape(fileHash)))
}

// ============================================================================
// NODES (distributed shards)
// ============================================================================

// Node represents a worker node in the distributed recording system.
type Node struct {
	NodeID          string `json:"node_id"`
	Hostname        string `json:"hostname"`
	InstanceLabel   string `json:"instance_label"`
	SoftwareVersion string `json:"software_version"`
	Status          string `json:"status"`
	CurrentLoad     int    `json:"current_load"`
	LastHeartbeat   string `json:"last_heartbeat,omitempty"`
	WebURL          string     `json:"web_url"`
	SessionDeadline *time.Time `json:"session_deadline,omitempty"`
	CreatedAt       string     `json:"created_at,omitempty"`
	UpdatedAt       string     `json:"updated_at,omitempty"`
}

// UpsertNode registers or updates a node.
func (c *Client) UpsertNode(node *Node) error {
	resp, err := c.requestWithRetry("POST", "/nodes?on_conflict=node_id", node)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// HeartbeatNode updates the last_heartbeat timestamp and current load for a node.
func (c *Client) HeartbeatNode(nodeID string, currentLoad int) error {
	return c.patch(fmt.Sprintf("/nodes?node_id=eq.%s", url.QueryEscape(nodeID)), map[string]interface{}{
		"last_heartbeat": "now()",
		"current_load":   currentLoad,
	})
}

// EnsureNodeOnline sets status=online for a node that is currently offline or
// unknown.  Used by the heartbeat loop to recover from a "stuck offline"
// state (e.g. reaper marked offline during a restart gap).  Does nothing if
// the node is already online or draining.
func (c *Client) EnsureNodeOnline(nodeID string) error {
	return c.patch(fmt.Sprintf("/nodes?node_id=eq.%s&status=neq.online&status=neq.draining", url.QueryEscape(nodeID)), map[string]interface{}{
		"status": "online",
	})
}

// UpdateNodeStatus changes the node's status (online/offline/draining).
func (c *Client) UpdateNodeStatus(nodeID, status string) error {
	return c.patch(fmt.Sprintf("/nodes?node_id=eq.%s", url.QueryEscape(nodeID)), map[string]interface{}{
		"status": status,
	})
}

// UpdateNodeWebURL sets the public web URL for a node.  Used by the cloudflared
// tunnel reporter so the admin panel's "Visit" link reflects the live tunnel.
func (c *Client) UpdateNodeWebURL(nodeID, webURL string) error {
	return c.patch(fmt.Sprintf("/nodes?node_id=eq.%s", url.QueryEscape(nodeID)), map[string]interface{}{
		"web_url": webURL,
	})
}

// GetNode retrieves a single node by ID.
func (c *Client) GetNode(nodeID string) (*Node, error) {
	var nodes []Node
	err := c.get(fmt.Sprintf("/nodes?node_id=eq.%s&limit=1", url.QueryEscape(nodeID)), &nodes)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}
	return &nodes[0], nil
}

// GetAllNodes returns all registered nodes, ordered by node_id.
func (c *Client) GetAllNodes() ([]Node, error) {
	var nodes []Node
	err := c.get("/nodes?order=node_id.asc", &nodes)
	return nodes, err
}

// GetAliveNodes returns all nodes with status=online and recent heartbeat.
func (c *Client) GetAliveNodes() ([]Node, error) {
	cutoff := time.Now().Add(-180 * time.Second).UTC().Format(time.RFC3339)
	var nodes []Node
	err := c.get(fmt.Sprintf("/nodes?status=eq.online&last_heartbeat=gt.%s&order=node_id.asc", url.QueryEscape(cutoff)), &nodes)
	return nodes, err
}

// GetDeadNodes returns node IDs whose heartbeat is older than the timeout.
// Nodes that are intentionally draining are excluded — they will release their
// channels as part of the normal shutdown sequence.
func (c *Client) GetDeadNodes(timeout time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-timeout).UTC().Format(time.RFC3339)
	var nodes []Node
	err := c.get(fmt.Sprintf("/nodes?last_heartbeat=lt.%s&status=neq.draining&select=node_id", url.QueryEscape(cutoff)), &nodes)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.NodeID
	}
	return ids, nil
}

// ============================================================================
// CHANNEL ASSIGNMENTS
// ============================================================================

// ChannelAssignment represents the assignment of a channel to a node.
type ChannelAssignment struct {
	Username      string `json:"username"`
	Site          string `json:"site"`
	AssignedNode  string `json:"assigned_node,omitempty"`
	Status        string `json:"status"`
	IsLive        bool   `json:"is_live"`
	LiveCheckedAt string `json:"live_checked_at,omitempty"`
	AssignedAt    string `json:"assigned_at,omitempty"`
	LastHeartbeat string `json:"last_heartbeat,omitempty"`
	// Config snapshot
	Framerate               int    `json:"framerate"`
	Resolution              int    `json:"resolution"`
	Pattern                 string `json:"pattern"`
	MaxDuration             int    `json:"max_duration"`
	MaxFilesize             int    `json:"max_filesize"`
	Compress                bool   `json:"compress"`
	MinDurationBeforeUpload int    `json:"min_duration_before_upload"`
	CreatedAt               string `json:"created_at,omitempty"`
	UpdatedAt               string `json:"updated_at,omitempty"`
}

// AssignmentStats holds summary statistics for fair-share calculation.
type AssignmentStats struct {
	TotalPoolChannels  int `json:"total_pool_channels"`
	TotalLiveChannels  int `json:"total_live_channels"`
	TotalUnassigned    int `json:"total_unassigned"`
	TotalAssignedNodes int `json:"total_assigned_nodes"`
	TotalAliveNodes    int `json:"total_alive_nodes"`
}

// ClaimChannels atomically claims up to `limit` unassigned channels for this node.
// Uses a PostgreSQL RPC function with SELECT ... FOR UPDATE SKIP LOCKED to prevent
// two nodes from claiming the same channel concurrently.
// Returns the rows that were successfully claimed (empty slice if none available).
func (c *Client) ClaimChannels(nodeID string, limit int) ([]ChannelAssignment, error) {
	body := map[string]interface{}{
		"p_node_id": nodeID,
		"p_limit":   limit,
	}

	resp, err := c.requestWithRetry("POST", "/rpc/claim_channels", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var claimed []ChannelAssignment
	if err := json.NewDecoder(resp.Body).Decode(&claimed); err != nil {
		return nil, fmt.Errorf("decode claimed: %w", err)
	}
	return claimed, nil
}

// ClaimSpecificChannel atomically claims one specific channel for this node.
// Uses a PostgreSQL RPC function with SELECT ... FOR UPDATE SKIP LOCKED to prevent
// two nodes from claiming the same channel concurrently.
// Returns true if the channel was successfully claimed, false if it was already taken.
func (c *Client) ClaimSpecificChannel(username, site, nodeID string) (bool, error) {
	body := map[string]interface{}{
		"p_username": username,
		"p_site":     site,
		"p_node_id":  nodeID,
	}

	resp, err := c.requestWithRetry("POST", "/rpc/claim_specific_channel", body)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var claimed []ChannelAssignment
	if err := json.NewDecoder(resp.Body).Decode(&claimed); err != nil {
		return false, fmt.Errorf("decode claimed: %w", err)
	}
	return len(claimed) > 0, nil
}

// ReleaseNodeChannels releases all channels currently assigned to a node.
// Does NOT filter by status so even orphaned rows (assigned_node set but
// status=unassigned) are correctly freed.
func (c *Client) ReleaseNodeChannels(nodeID string) error {
	return c.patch(fmt.Sprintf("/channel_assignments?assigned_node=eq.%s", url.QueryEscape(nodeID)),
		map[string]interface{}{
			"assigned_node": nil,
			"status":        "unassigned",
		})
}

// ReleaseExcessChannels releases up to `limit` channels from this node back to unassigned.
// Uses a two-step approach (GET usernames, then PATCH by username) because
// PostgREST PATCH ignores the `limit` parameter.
func (c *Client) ReleaseExcessChannels(nodeID string, limit int) ([]ChannelAssignment, error) {
	// Step 1: GET the usernames we want to release.
	// Prioritise releasing offline channels first (is_live=false), then online
	// ones.  Within each group we pick alphabetically to be deterministic.
	var offline, online []ChannelAssignment

	err := c.get(
		fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&status=neq.unassigned&is_live=eq.false&select=username,site&order=username.asc&limit=%d",
			url.QueryEscape(nodeID), limit), &offline)
	if err != nil {
		return nil, err
	}

	remaining := limit - len(offline)
	if remaining > 0 {
		err = c.get(
			fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&status=neq.unassigned&is_live=eq.true&select=username,site&order=username.asc&limit=%d",
				url.QueryEscape(nodeID), remaining), &online)
		if err != nil {
			return nil, err
		}
	}

	target := append(offline, online...)
	if len(target) == 0 {
		return nil, nil
	}

	// Step 2: PATCH only those specific channels
	usernames := make([]string, len(target))
	for i, ca := range target {
		usernames[i] = ca.Username
	}

	resp, err := c.requestWithRetry("PATCH",
		fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&username=in.(%s)",
			url.QueryEscape(nodeID), joinEscaped(usernames)),
		map[string]interface{}{
			"assigned_node": nil,
			"status":        "unassigned",
		})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var released []ChannelAssignment
	if err := json.NewDecoder(resp.Body).Decode(&released); err != nil {
		return nil, fmt.Errorf("decode released: %w", err)
	}
	return released, nil
}

// ReleaseChannel releases a single channel back to the pool.
func (c *Client) ReleaseChannel(username, site string) error {
	return c.patch(fmt.Sprintf("/channel_assignments?username=eq.%s&site=eq.%s", url.QueryEscape(username), url.QueryEscape(site)),
		map[string]interface{}{
			"assigned_node": nil,
			"status":        "unassigned",
		})
}

// RepairOrphanedAssignments fixes rows where assigned_node is set but
// status is still 'unassigned'. This can happen if a claim was partially
// rolled back (assigned_node written, status not updated) or if a
// release set status=unassigned without nulling assigned_node. These rows
// are invisible to both ClaimChannels (which requires assigned_node IS NULL)
// and ReleaseExcessChannels (which requires status != unassigned), causing
// a permanent deadlock.
//
// Returns the number of rows repaired.
func (c *Client) RepairOrphanedAssignments() (int, error) {
	// Step 1: count the broken rows
	var orphaned []ChannelAssignment
	err := c.get("/channel_assignments?assigned_node=not.is.null&status=eq.unassigned&select=username&limit=50000", &orphaned)
	if err != nil {
		return 0, err
	}
	if len(orphaned) == 0 {
		return 0, nil
	}

	// Step 2: null out assigned_node on all broken rows
	err = c.patch("/channel_assignments?assigned_node=not.is.null&status=eq.unassigned",
		map[string]interface{}{
			"assigned_node": nil,
		})
	if err != nil {
		return 0, err
	}

	return len(orphaned), nil
}

// DeleteAssignment removes a channel assignment entirely from the pool.
func (c *Client) DeleteAssignment(username, site string) error {
	return c.delete(fmt.Sprintf("/channel_assignments?username=eq.%s&site=eq.%s", url.QueryEscape(username), url.QueryEscape(site)))
}

// GetNodeAssignments returns all channel assignments for a given node.
func (c *Client) GetNodeAssignments(nodeID string) ([]ChannelAssignment, error) {
	var assignments []ChannelAssignment
	err := c.get(fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&order=username.asc", url.QueryEscape(nodeID)), &assignments)
	return assignments, err
}

// GetAssignment returns the assignment for a specific channel.
func (c *Client) GetAssignment(username, site string) (*ChannelAssignment, error) {
	var assignments []ChannelAssignment
	err := c.get(fmt.Sprintf("/channel_assignments?username=eq.%s&site=eq.%s&limit=1",
		url.QueryEscape(username), url.QueryEscape(site)), &assignments)
	if err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return nil, nil
	}
	return &assignments[0], nil
}

// GetAssignmentsByStatus returns all assignments with a given status.
func (c *Client) GetAssignmentsByStatus(status string) ([]ChannelAssignment, error) {
	var assignments []ChannelAssignment
	err := c.get(fmt.Sprintf("/channel_assignments?status=eq.%s&order=username.asc", url.QueryEscape(status)), &assignments)
	return assignments, err
}

// GetAllAssignments returns all channel assignments.
func (c *Client) GetAllAssignments() ([]ChannelAssignment, error) {
	var assignments []ChannelAssignment
	err := c.get("/channel_assignments?order=username.asc&limit=50000", &assignments)
	return assignments, err
}

// GetAssignmentStats returns total live channels and total alive nodes for fair-share calculation.
func (c *Client) GetAssignmentStats() (*AssignmentStats, error) {
	stats := &AssignmentStats{}

	// Count all channels in the pool (used for informational purposes)
	var all []ChannelAssignment
	err := c.get("/channel_assignments?select=username&limit=50000", &all)
	if err != nil {
		return nil, err
	}
	stats.TotalPoolChannels = len(all)

	// Count only live channels for fair-share distribution
	var live []ChannelAssignment
	err = c.get("/channel_assignments?is_live=eq.true&select=username&limit=50000", &live)
	if err != nil {
		return nil, err
	}
	stats.TotalLiveChannels = len(live)

	// Count total unassigned channels
	var unassigned []ChannelAssignment
	err = c.get("/channel_assignments?status=eq.unassigned&select=username&limit=50000", &unassigned)
	if err != nil {
		return nil, err
	}
	stats.TotalUnassigned = len(unassigned)

	// Count nodes with active assignments
	var assigned []ChannelAssignment
	err = c.get("/channel_assignments?assigned_node=not.is.null&select=assigned_node&limit=50000", &assigned)
	if err != nil {
		return nil, err
	}
	assignedNodes := make(map[string]bool)
	for _, a := range assigned {
		assignedNodes[a.AssignedNode] = true
	}
	stats.TotalAssignedNodes = len(assignedNodes)

	// Count alive nodes
	aliveNodes, err := c.GetAliveNodes()
	if err != nil {
		return nil, err
	}
	stats.TotalAliveNodes = len(aliveNodes)

	return stats, nil
}

// CountMyAssignments returns the number of channels assigned to a node.
// Uses a count query rather than loading all rows.
func (c *Client) CountMyAssignments(nodeID string) (int, error) {
	var assignments []ChannelAssignment
	err := c.get(fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&status=neq.unassigned&select=username&limit=50000",
		url.QueryEscape(nodeID)), &assignments)
	if err != nil {
		return 0, err
	}
	return len(assignments), nil
}

// SetChannelsLive bulk-updates is_live=true for the given usernames.
func (c *Client) SetChannelsLive(usernames []string) error {
	if len(usernames) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return c.patch(
		fmt.Sprintf("/channel_assignments?username=in.(%s)&is_live=eq.false",
			joinEscaped(usernames)),
		map[string]interface{}{
			"is_live":         true,
			"live_checked_at": now,
		})
}

// SetChannelsNotLive bulk-updates is_live=false for channels NOT in the given list.
func (c *Client) SetChannelsNotLive(usernames []string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if len(usernames) == 0 {
		// Mark all as not live
		return c.patch("/channel_assignments?is_live=eq.true",
			map[string]interface{}{
				"is_live":         false,
				"live_checked_at": now,
			})
	}
	return c.patch(
		fmt.Sprintf("/channel_assignments?username=not.in.(%s)&is_live=eq.true",
			joinEscaped(usernames)),
		map[string]interface{}{
			"is_live":         false,
			"live_checked_at": now,
		})
}

// ReclaimChannels sets assigned_node=NULL for all channels belonging to a dead node.
// Returns the number of channels reclaimed.
func (c *Client) ReclaimChannels(deadNodeID string) (int, error) {
	// First, count what we'll reclaim
	var assignments []ChannelAssignment
	err := c.get(fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&select=username&limit=50000",
		url.QueryEscape(deadNodeID)), &assignments)
	if err != nil {
		return 0, err
	}
	if len(assignments) == 0 {
		return 0, nil
	}

	// Release them
	if err := c.ReleaseNodeChannels(deadNodeID); err != nil {
		return 0, err
	}
	return len(assignments), nil
}

// GetNodesWithImminentDeadline returns online nodes whose session_deadline is
// within `window` from now. Used to migrate a node's channels away BEFORE the
// node is killed (e.g. GitHub's 6-hour runner limit).
func (c *Client) GetNodesWithImminentDeadline(window time.Duration) ([]Node, error) {
	cutoff := time.Now().Add(window).UTC().Format(time.RFC3339)
	var nodes []Node
	err := c.get(fmt.Sprintf("/nodes?session_deadline=not.is.null&session_deadline=lt.%s&status=eq.online&order=node_id.asc",
		url.QueryEscape(cutoff)), &nodes)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// ReleaseExcessOfflineChannels releases up to `limit` OFFLINE channels from this
// node back to unassigned. Unlike ReleaseExcessChannels it NEVER releases a
// live or recording channel, so a node's in-progress recordings are left alone
// during fair-share rebalancing. Channels are selected offline-first, then
// alphabetically, to be deterministic.
func (c *Client) ReleaseExcessOfflineChannels(nodeID string, limit int) ([]ChannelAssignment, error) {
	if limit <= 0 {
		return nil, nil
	}

	// Offline channels only, and exclude any still marked 'recording' (a node may
	// briefly lag the liveness flag, so this protects a channel that is actually
	// being recorded from being interrupted).
	var offline []ChannelAssignment
	err := c.get(
		fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&status=neq.unassigned&status=neq.recording&is_live=eq.false&select=username,site&order=username.asc&limit=%d",
			url.QueryEscape(nodeID), limit), &offline)
	if err != nil {
		return nil, err
	}
	if len(offline) == 0 {
		return nil, nil
	}

	usernames := make([]string, len(offline))
	for i, ca := range offline {
		usernames[i] = ca.Username
	}

	resp, err := c.requestWithRetry("PATCH",
		fmt.Sprintf("/channel_assignments?assigned_node=eq.%s&username=in.(%s)",
			url.QueryEscape(nodeID), joinEscaped(usernames)),
		map[string]interface{}{
			"assigned_node": nil,
			"status":        "unassigned",
		})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var released []ChannelAssignment
	if err := json.NewDecoder(resp.Body).Decode(&released); err != nil {
		return nil, fmt.Errorf("decode released: %w", err)
	}
	return released, nil
}

// MarkChannelRecording marks a channel as actively recording on its node and
// bumps its recording heartbeat. Called by the liveness loop for this node's
// live channels so "live+recording" is authoritative in the DB.
func (c *Client) MarkChannelRecording(username, site string) error {
	return c.patch(
		fmt.Sprintf("/channel_assignments?username=eq.%s&site=eq.%s",
			url.QueryEscape(username), url.QueryEscape(site)),
		map[string]interface{}{
			"status":           "recording",
			"last_recorded_at": "now()",
			"last_heartbeat":   "now()",
		})
}

// SetChannelStatus updates the status of a single channel assignment.
func (c *Client) SetChannelStatus(username, site, status string) error {
	return c.patch(
		fmt.Sprintf("/channel_assignments?username=eq.%s&site=eq.%s",
			url.QueryEscape(username), url.QueryEscape(site)),
		map[string]interface{}{"status": status})
}

// ReassignChannel atomically moves a channel from one node to another via the
// reassign_channel RPC (SELECT ... FOR UPDATE SKIP LOCKED). If the channel is no
// longer owned by pFromNode (e.g. already reaped/migrated), this is a no-op.
func (c *Client) ReassignChannel(username, site, fromNode, toNode string) error {
	body := map[string]interface{}{
		"p_username":  username,
		"p_site":      site,
		"p_from_node": fromNode,
		"p_to_node":   toNode,
	}
	resp, err := c.requestWithRetry("POST", "/rpc/reassign_channel", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// BulkInsertAssignments creates channel_assignments rows for channels that don't have one yet.
func (c *Client) BulkInsertAssignments(assignments []ChannelAssignment) error {
	if len(assignments) == 0 {
		return nil
	}
	resp, err := c.requestWithRetry("POST", "/channel_assignments?on_conflict=username,site", assignments)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ============================================================================
// CHANNEL POOL (shared app_settings key)
// ============================================================================

// PoolKey returns the app_settings key for the shared channel pool.
func PoolKey() string {
	return "channel_pool"
}

// LoadPoolFromDB reads the shared channel pool from app_settings.
func (c *Client) LoadPoolFromDB() ([]byte, error) {
	var settings []AppSetting
	err := c.get(fmt.Sprintf("/app_settings?key=eq.%s&limit=1", PoolKey()), &settings)
	if err != nil {
		return nil, err
	}
	if len(settings) == 0 {
		return nil, nil
	}
	return settings[0].Value, nil
}

// SavePoolToDB writes the shared channel pool to app_settings.
func (c *Client) SavePoolToDB(data []byte) error {
	return c.SaveSetting(PoolKey(), json.RawMessage(data))
}

// GetAllSettingKeys returns all app_settings keys matching a LIKE pattern.
// The prefix should be like "channels_" to get all instance-scoped keys.
func (c *Client) GetAllSettingKeys(likePattern string) ([]string, error) {
	// Supabase REST doesn't support LIKE directly, so we fetch all keys
	// and filter client-side. For typical deployments this is < 100 keys.
	var settings []AppSetting
	err := c.get("/app_settings?select=key&limit=50000", &settings)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, s := range settings {
		if strings.HasPrefix(s.Key, likePattern) {
			matches = append(matches, s.Key)
		}
	}
	return matches, nil
}

// DeleteLegacyChannelsBlob deletes the legacy isolated-mode channels blob
// from app_settings. The old blob would cause migrateLegacyChannels to
// re-insert channels into the pool on every startup.
func (c *Client) DeleteLegacyChannelsBlob(key string) error {
	return c.delete("/app_settings?key=eq." + key)
}

// joinEscaped joins strings with Supabase-compatible CSV escaping.
func joinEscaped(items []string) string {
	escaped := make([]string, len(items))
	for i, item := range items {
		escaped[i] = url.QueryEscape(item)
	}
	return strings.Join(escaped, ",")
}

// ============================================================================
// HEALTH CHECK
// ============================================================================

// HealthCheck verifies the database connection
func (c *Client) HealthCheck() error {
	resp, err := c.request(context.Background(), "GET", "/app_settings?key=eq.__healthcheck__&select=key&limit=1", nil)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		return nil
	case 404:
		return fmt.Errorf("app_settings table not found (HTTP 404) — run the SQL migration first")
	case 401, 403:
		return fmt.Errorf("authentication failed (HTTP %d) — check SUPABASE_API_KEY and RLS policies", resp.StatusCode)
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected response (HTTP %d): %s", resp.StatusCode, string(body))
	}
}
