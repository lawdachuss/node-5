package server

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/teacat/chaturbate-dvr/entity"
)

var Config *entity.Config
var configMu sync.RWMutex

type persistedSettings struct {
	Cookies   string `json:"cookies"`
	UserAgent string `json:"user_agent"`
	ByparrURL string `json:"byparr_url"`
	StreamtapeLogin string `json:"streamtape_login,omitempty"`
	StreamtapeKey   string `json:"streamtape_key,omitempty"`
	MixdropEmail    string `json:"mixdrop_email,omitempty"`
	MixdropToken    string `json:"mixdrop_token,omitempty"`
	PixelDrainToken string `json:"pixeldrain_token,omitempty"`
}

// SaveSettings writes the runtime cookies and user-agent to Supabase.
func SaveSettings() error {
	configMu.RLock()
	s := persistedSettings{
		Cookies:   Config.Cookies,
		UserAgent: Config.UserAgent,
		ByparrURL:        Config.ByparrURL,
		StreamtapeLogin:  Config.StreamtapeLogin,
		StreamtapeKey:    Config.StreamtapeKey,
		MixdropEmail:     Config.MixdropEmail,
		MixdropToken:     Config.MixdropToken,
		PixelDrainToken:  Config.PixelDrainToken,
	}
	configMu.RUnlock()

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := SaveSettingsToDB(b); err != nil {
		return fmt.Errorf("save settings to Supabase: %w", err)
	}
	return nil
}

// LoadSettings reads persisted cookies and user-agent from Supabase.
func LoadSettings() error {
	b := LoadSettingsFromDB()
	if b == nil {
		return nil
	}

	var s persistedSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("unmarshal settings: %w", err)
	}

	configMu.Lock()
	if s.Cookies != "" {
		Config.Cookies = s.Cookies
	}
	if s.UserAgent != "" {
		Config.UserAgent = s.UserAgent
	}
	if s.ByparrURL != "" {
		Config.ByparrURL = s.ByparrURL
	}
	if s.StreamtapeLogin != "" {
		Config.StreamtapeLogin = s.StreamtapeLogin
	}
	if s.StreamtapeKey != "" {
		Config.StreamtapeKey = s.StreamtapeKey
	}
	if s.MixdropEmail != "" {
		Config.MixdropEmail = s.MixdropEmail
	}
	if s.MixdropToken != "" {
		Config.MixdropToken = s.MixdropToken
	}
	if s.PixelDrainToken != "" {
		Config.PixelDrainToken = s.PixelDrainToken
	}
	configMu.Unlock()

	return nil
}

// UpdateByparrCredentials safely updates cookies and user-agent
// with mutex protection for concurrent access.
func UpdateByparrCredentials(cookies, userAgent string) {
	configMu.Lock()
	if cookies != "" {
		Config.Cookies = cookies
	}
	if userAgent != "" {
		Config.UserAgent = userAgent
	}
	configMu.Unlock()
}

// UpdateUploaderCredentials updates upload service credentials (Streamtape, Mixdrop)
// and protects concurrent access with a mutex.
func UpdateUploaderCredentials(streamtapeLogin, streamtapeKey, mixdropEmail, mixdropToken, pixeldrainToken string) {
	configMu.Lock()
	if streamtapeLogin != "" {
		Config.StreamtapeLogin = streamtapeLogin
	}
	if streamtapeKey != "" {
		Config.StreamtapeKey = streamtapeKey
	}
	if mixdropEmail != "" {
		Config.MixdropEmail = mixdropEmail
	}
	if mixdropToken != "" {
		Config.MixdropToken = mixdropToken
	}
	if pixeldrainToken != "" {
		Config.PixelDrainToken = pixeldrainToken
	}
	configMu.Unlock()
}
