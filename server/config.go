package server

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/teacat/chaturbate-dvr/entity"
)

var Config *entity.Config

const settingsPath = "./conf/settings.json"

type persistedSettings struct {
	Cookies   string `json:"cookies"`
	UserAgent string `json:"user_agent"`
	ByparrURL string `json:"byparr_url"`
}

// SaveSettings writes the runtime cookies and user-agent to disk AND to Supabase.
func SaveSettings() error {
	s := persistedSettings{
		Cookies:   Config.Cookies,
		UserAgent: Config.UserAgent,
		ByparrURL: Config.ByparrURL,
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	// Save to Supabase
	if err := SaveSettingsToDB(b); err != nil {
		fmt.Printf("[WARN] [db] could not save settings to Supabase: %v\n", err)
	}

	// Also save to local file as backup
	if mkErr := os.MkdirAll("./conf", 0777); mkErr == nil {
		os.WriteFile(settingsPath, b, 0666)
	}

	return nil
}

// LoadSettings reads persisted cookies and user-agent from Supabase (or local file fallback).
func LoadSettings() error {
	var b []byte

	// Try Supabase first
	if dbData := LoadSettingsFromDB(); dbData != nil {
		fmt.Println(" INFO [db] loaded settings from Supabase")
		b = dbData
	} else {
		// Fall back to local file
		data, err := os.ReadFile(settingsPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read settings: %w", err)
		}
		b = data
	}

	var s persistedSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("unmarshal settings: %w", err)
	}
	if Config.Cookies == "" && s.Cookies != "" {
		Config.Cookies = s.Cookies
	}
	if Config.UserAgent == "" && s.UserAgent != "" {
		Config.UserAgent = s.UserAgent
	}
	if Config.ByparrURL == "" && s.ByparrURL != "" {
		Config.ByparrURL = s.ByparrURL
	}
	return nil
}
