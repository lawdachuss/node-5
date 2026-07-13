package server

import (
	"sync"
	"testing"
	"time"
)

func TestClassifyLog(t *testing.T) {
	cases := []struct {
		line         string
		wantLevel    string
		wantUsername string
	}{
		{"ERROR [Fucking_Naughty_Couple] upload failed: use of closed network connection", "error", "Fucking_Naughty_Couple"},
		{" WARN [denobluora] log queue full, dropped: x", "warn", "denobluora"},
		{" INFO [poompoompeach_] duration: 0:51:51, filesize: 1.09 GB", "info", "poompoompeach_"},
		{"[proxy] all proxies failed — refreshed proxy list from env", "info", ""},
		{" [GIN] 2026/07/13 - 09:30 | 200 | GET \"/\"", "info", ""},
		{"[startup] config loaded in 12ms", "info", ""},
		{"panic: runtime error: invalid memory address", "error", ""},
		{"FATAL: something broke", "error", ""},
		{"[coordinator] reclaimed 3 channel(s) from dead node", "info", ""},
	}
	for _, c := range cases {
		level, user := classifyLog(c.line)
		if level != c.wantLevel {
			t.Errorf("classifyLog(%q).level = %q, want %q", c.line, level, c.wantLevel)
		}
		if user != c.wantUsername {
			t.Errorf("classifyLog(%q).username = %q, want %q", c.line, user, c.wantUsername)
		}
	}
}

func TestLogBufferSink(t *testing.T) {
	buf := NewLogBuffer(100)

	var mu sync.Mutex
	var got []parsedLog
	SetLogSink(func(level, username, message string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, parsedLog{level, username, message})
	})
	defer SetLogSink(nil)

	buf.Write([]byte("ERROR [chanA] boom\nINFO [chanB] ok\n[proxy] scanning 2467 proxies\n"))

	// Give the drain goroutine time to process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 sink lines, got %d", len(got))
	}
	if got[0].level != "error" || got[0].username != "chanA" || got[0].message != "ERROR [chanA] boom" {
		t.Errorf("unexpected line 0: %+v", got[0])
	}
	if got[1].level != "info" || got[1].username != "chanB" {
		t.Errorf("unexpected line 1: %+v", got[1])
	}
	if got[2].username != "" {
		t.Errorf("expected empty username for proxy line, got %q", got[2].username)
	}
}
