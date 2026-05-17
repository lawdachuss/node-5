package server

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

var db *sql.DB

// InitDB connects to the PostgreSQL database (DATABASE_URL) and creates the
// channels table if it does not exist. Silently skips if DATABASE_URL is unset.
func InitDB() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println(" INFO [db] DATABASE_URL not set — channel persistence will use file only")
		return nil
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("db open: %w", err)
	}

	if err := db.Ping(); err != nil {
		db = nil
		return fmt.Errorf("db ping: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dvr_channels (
		key  TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
	if err != nil {
		db = nil
		return fmt.Errorf("db create table: %w", err)
	}

	fmt.Println(" INFO [db] connected — channels will be persisted in PostgreSQL")
	return nil
}

// SaveChannelsToDB upserts the channels JSON blob under the key "channels".
func SaveChannelsToDB(data []byte) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`
		INSERT INTO dvr_channels (key, value) VALUES ('channels', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
	`, string(data))
	return err
}

// LoadChannelsFromDB returns the channels JSON blob, or nil if not found.
func LoadChannelsFromDB() []byte {
	if db == nil {
		return nil
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM dvr_channels WHERE key = 'channels'`).Scan(&value); err != nil {
		return nil
	}
	return []byte(value)
}
