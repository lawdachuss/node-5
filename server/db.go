package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var supabaseClient *supabaseREST

type supabaseREST struct {
	url    string
	apiKey string
	http   *http.Client
}

// InitDB initialises the Supabase REST client.
// Falls back silently if SUPABASE_URL / SUPABASE_API_KEY are not set.
func InitDB() error {
	url := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_API_KEY")
	if url == "" || key == "" {
		fmt.Println(" INFO [db] SUPABASE_URL/SUPABASE_API_KEY not set — persistence will use local files only")
		return nil
	}

	supabaseClient = &supabaseREST{
		url:    url,
		apiKey: key,
		http:   &http.Client{Timeout: 15 * time.Second},
	}

	// Quick connectivity check
	if err := supabaseClient.ping(); err != nil {
		supabaseClient = nil
		return fmt.Errorf("supabase ping: %w", err)
	}

	fmt.Println(" INFO [db] connected to Supabase — all data will be persisted remotely")
	return nil
}

// ping checks that the Supabase REST endpoint is reachable.
func (s *supabaseREST) ping() error {
	req, _ := http.NewRequest("GET", s.url+"/rest/v1/channels?select=id&limit=1", nil)
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (s *supabaseREST) setHeaders(req *http.Request) {
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")
}

func (s *supabaseREST) get(table, query string) ([]byte, error) {
	req, _ := http.NewRequest("GET", s.url+"/rest/v1/"+table+"?"+query, nil)
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (s *supabaseREST) upsert(table string, body interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest("POST", s.url+"/rest/v1/"+table, bytes.NewReader(b))
	s.setHeaders(req)
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase upsert %s: status %d: %s", table, resp.StatusCode, body)
	}
	return nil
}

func (s *supabaseREST) delete(table, filter string) error {
	req, _ := http.NewRequest("DELETE", s.url+"/rest/v1/"+table+"?"+filter, nil)
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ─── Channels ────────────────────────────────────────────────────────────────

// supabaseChannel mirrors the `channels` table schema in Supabase.
type supabaseChannel struct {
	Username    string `json:"username"`
	Site        string `json:"site"`
	IsPaused    bool   `json:"is_paused"`
	Framerate   int    `json:"framerate"`
	Resolution  int    `json:"resolution"`
	Pattern     string `json:"pattern"`
	MaxDuration int    `json:"max_duration"`
	MaxFilesize int    `json:"max_filesize"`
	CreatedAt   int64  `json:"created_at"`
}

// channelConfig is the shape stored in conf/channels.json.
type channelConfig struct {
	IsPaused    bool   `json:"is_paused"`
	Username    string `json:"username"`
	Framerate   int    `json:"framerate"`
	Resolution  int    `json:"resolution"`
	Pattern     string `json:"pattern"`
	MaxDuration int    `json:"max_duration"`
	MaxFilesize int    `json:"max_filesize"`
	Compress    bool   `json:"compress"`
	CreatedAt   int64  `json:"created_at"`
}

// SaveChannelsToDB upserts the channel list to Supabase.
// The JSON blob is the same format written to conf/channels.json.
func SaveChannelsToDB(data []byte) error {
	if supabaseClient == nil {
		return nil
	}
	var configs []channelConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("parse channels json: %w", err)
	}

	// Build the set of usernames to keep, then replace all rows.
	rows := make([]supabaseChannel, 0, len(configs))
	for _, c := range configs {
		rows = append(rows, supabaseChannel{
			Username:    c.Username,
			Site:        "chaturbate",
			IsPaused:    c.IsPaused,
			Framerate:   c.Framerate,
			Resolution:  c.Resolution,
			Pattern:     c.Pattern,
			MaxDuration: c.MaxDuration,
			MaxFilesize: c.MaxFilesize,
			CreatedAt:   c.CreatedAt,
		})
	}

	if len(rows) == 0 {
		// Delete all channels when the list is empty
		return supabaseClient.delete("channels", "id=gte.0")
	}

	return supabaseClient.upsert("channels", rows)
}

// LoadChannelsFromDB fetches channels from Supabase and returns them as
// the same JSON format used by conf/channels.json, or nil if unavailable.
func LoadChannelsFromDB() []byte {
	if supabaseClient == nil {
		return nil
	}
	data, err := supabaseClient.get("channels", "select=username,site,is_paused,framerate,resolution,pattern,max_duration,max_filesize,created_at&order=created_at.asc")
	if err != nil {
		fmt.Printf("[WARN] [db] load channels: %v\n", err)
		return nil
	}

	// Parse Supabase rows back into channelConfig slice
	var rows []supabaseChannel
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return nil
	}

	configs := make([]channelConfig, 0, len(rows))
	for _, r := range rows {
		configs = append(configs, channelConfig{
			Username:    r.Username,
			IsPaused:    r.IsPaused,
			Framerate:   r.Framerate,
			Resolution:  r.Resolution,
			Pattern:     r.Pattern,
			MaxDuration: r.MaxDuration,
			MaxFilesize: r.MaxFilesize,
			CreatedAt:   r.CreatedAt,
		})
	}

	b, err := json.Marshal(configs)
	if err != nil {
		return nil
	}
	return b
}

// ─── Settings ────────────────────────────────────────────────────────────────

// supabaseSetting stores a key/value pair in the `app_settings` table.
// If that table doesn't exist in Supabase the calls are silently ignored.
type supabaseSetting struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// SaveSettingsToDB upserts the settings JSON blob into app_settings.
func SaveSettingsToDB(data []byte) error {
	if supabaseClient == nil {
		return nil
	}
	row := supabaseSetting{Key: "dvr_settings", Value: json.RawMessage(data)}
	if err := supabaseClient.upsert("app_settings", row); err != nil {
		// app_settings table may not exist — not fatal
		fmt.Printf("[WARN] [db] save settings: %v\n", err)
	}
	return nil
}

// LoadSettingsFromDB fetches the settings blob from app_settings.
func LoadSettingsFromDB() []byte {
	if supabaseClient == nil {
		return nil
	}
	data, err := supabaseClient.get("app_settings", "select=value&key=eq.dvr_settings&limit=1")
	if err != nil {
		return nil
	}
	var rows []supabaseSetting
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return nil
	}
	return []byte(rows[0].Value)
}

// ─── Recordings ──────────────────────────────────────────────────────────────

// supabaseVideoUpload mirrors the `video_uploads` table schema.
type supabaseVideoUpload struct {
	ID              int    `json:"id,omitempty"`
	StreamerName    string `json:"streamer_name"`
	Filename        string `json:"filename,omitempty"`
	GofileLink      string `json:"gofile_link,omitempty"`
	TurboViPlayLink string `json:"turboviplay_link,omitempty"`
	VoeSXLink       string `json:"voesx_link,omitempty"`
	StreamtapeLink  string `json:"streamtape_link,omitempty"`
	ThumbnailLink   string `json:"thumbnail_link,omitempty"`
	UploadDate      string `json:"upload_date,omitempty"`
}

// recDBShape is the in-memory recordings DB shape (matches channel_upload.go / videos_handler.go).
type recDBShape struct {
	Version  int                        `json:"version"`
	Channels map[string]*recChanShape   `json:"channels"`
}

type recChanShape struct {
	Gender     string          `json:"gender"`
	Recordings []recEntryShape `json:"recordings"`
}

type recEntryShape struct {
	Filename     string            `json:"filename"`
	Timestamp    string            `json:"timestamp"`
	RoomTitle    string            `json:"room_title"`
	Tags         []string          `json:"tags"`
	Viewers      int               `json:"viewers"`
	Resolution   string            `json:"resolution"`
	Framerate    int               `json:"framerate"`
	Links        map[string]string `json:"links"`
	ThumbnailURL string            `json:"thumbnail_url"`
	SpriteURL    string            `json:"sprite_url"`
	EmbedURL     string            `json:"embed_url"`
	Filesize     int64             `json:"filesize"`
}

// SaveRecordingsToDB syncs the recordings JSON blob to the video_uploads table.
func SaveRecordingsToDB(data []byte) error {
	if supabaseClient == nil {
		return nil
	}
	var db recDBShape
	if err := json.Unmarshal(data, &db); err != nil {
		return fmt.Errorf("parse recordings json: %w", err)
	}

	for username, chanData := range db.Channels {
		for _, rec := range chanData.Recordings {
			row := supabaseVideoUpload{
				StreamerName:    username,
				Filename:        rec.Filename,
				ThumbnailLink:   rec.ThumbnailURL,
			}
			if l, ok := rec.Links["GoFile"]; ok {
				row.GofileLink = l
			}
			if l, ok := rec.Links["TurboViPlay"]; ok {
				row.TurboViPlayLink = l
			}
			if l, ok := rec.Links["VOE.sx"]; ok {
				row.VoeSXLink = l
			}
			if l, ok := rec.Links["VoeSX"]; ok {
				row.VoeSXLink = l
			}
			if l, ok := rec.Links["Streamtape"]; ok {
				row.StreamtapeLink = l
			}

			// Upsert by filename — delete existing row first then insert
			// (Supabase upsert on non-primary key requires a unique constraint;
			//  filename is not unique by default so we use a simple delete+insert)
			supabaseClient.delete("video_uploads", "filename=eq."+rec.Filename)
			if err := supabaseClient.upsert("video_uploads", row); err != nil {
				fmt.Printf("[WARN] [db] save recording %s: %v\n", rec.Filename, err)
			}
		}
	}
	return nil
}

// LoadRecordingsFromDB fetches video_uploads rows and converts them back to
// the recordings JSON format used by the app.
func LoadRecordingsFromDB() []byte {
	if supabaseClient == nil {
		return nil
	}
	data, err := supabaseClient.get("video_uploads",
		"select=streamer_name,filename,gofile_link,turboviplay_link,voesx_link,streamtape_link,thumbnail_link,upload_date&order=upload_date.desc")
	if err != nil {
		fmt.Printf("[WARN] [db] load recordings: %v\n", err)
		return nil
	}

	var rows []supabaseVideoUpload
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return nil
	}

	db := recDBShape{
		Version:  2,
		Channels: map[string]*recChanShape{},
	}

	for _, row := range rows {
		ch, ok := db.Channels[row.StreamerName]
		if !ok {
			ch = &recChanShape{Recordings: []recEntryShape{}}
			db.Channels[row.StreamerName] = ch
		}
		links := map[string]string{}
		if row.GofileLink != "" {
			links["GoFile"] = row.GofileLink
		}
		if row.TurboViPlayLink != "" {
			links["TurboViPlay"] = row.TurboViPlayLink
		}
		if row.VoeSXLink != "" {
			links["VOE.sx"] = row.VoeSXLink
		}
		if row.StreamtapeLink != "" {
			links["Streamtape"] = row.StreamtapeLink
		}
		ch.Recordings = append(ch.Recordings, recEntryShape{
			Filename:     row.Filename,
			Timestamp:    row.UploadDate,
			Links:        links,
			ThumbnailURL: row.ThumbnailLink,
			Tags:         []string{},
		})
	}

	b, _ := json.Marshal(db)
	return b
}
