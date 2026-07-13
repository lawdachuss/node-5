package server

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type logEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level,omitempty"`
	Message string    `json:"message"`
}

type LogBuffer struct {
	mu    sync.RWMutex
	lines []logEntry
	cap   int
}

var globalLogBuffer = NewLogBuffer(5000)

func GetLogBuffer() *LogBuffer {
	return globalLogBuffer
}

func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		lines: make([]logEntry, 0, capacity),
		cap:   capacity,
	}
}

// ─── Persistent log sink ────────────────────────────────────────────────────
//
// Every line written to the buffer is also offered to a registered LogSink
// (e.g. Supabase channel_logs) so logs survive restarts and can be queried
// across all nodes. The sink runs in a background goroutine and is non-blocking:
// on backpressure the line is dropped rather than stalling the logging hot path.

// LogSink receives a parsed log line for persistent storage.
// level is one of "error", "warn", "info". username is the bracketed channel
// name when present, otherwise empty.
type LogSink func(level, username, message string)

var (
	logSinkMu sync.Mutex
	logSink   LogSink
	logSinkCh chan parsedLog
)

type parsedLog struct {
	level, username, message string
}

// SetLogSink registers a persistent log sink. Passing nil disables persistence.
// Safe to call once during startup before heavy logging begins.
func SetLogSink(fn LogSink) {
	logSinkMu.Lock()
	defer logSinkMu.Unlock()
	if fn == nil {
		logSink = nil
		return
	}
	logSink = fn
	if logSinkCh == nil {
		logSinkCh = make(chan parsedLog, 4096)
		go logSinkDrain()
	}
}

func logSinkDrain() {
	for pl := range logSinkCh {
		logSinkMu.Lock()
		fn := logSink
		logSinkMu.Unlock()
		if fn != nil {
			fn(pl.level, pl.username, pl.message)
		}
	}
}

// systemLogTags are bracketed tokens that denote a subsystem rather than a
// channel username, so they should not be stored as the log's username.
var systemLogTags = map[string]bool{
	"proxy": true, "GIN": true, "startup": true, "coordinator": true,
	"reaper": true, "WARN": true, "DEBUG": true, "INFO": true,
	"ERROR": true, "upload": true, "manager": true, "server": true,
}

// classifyLog extracts a log level and (when present) the channel username
// from a raw log line.
func classifyLog(line string) (level, username string) {
	level = "info"
	switch {
	case strings.Contains(strings.ToUpper(line), "PANIC"),
		strings.Contains(strings.ToUpper(line), "FATAL"):
		level = "error"
	case strings.Contains(line, "ERROR ["),
		strings.HasPrefix(strings.TrimSpace(line), "ERROR "):
		level = "error"
	case strings.Contains(line, " WARN "),
		strings.Contains(line, "[WARN]"),
		strings.Contains(line, " WARN ["):
		level = "warn"
	}

	if i := strings.Index(line, "["); i >= 0 {
		if j := strings.Index(line[i:], "]"); j > 1 {
			tok := strings.TrimSpace(line[i+1 : i+j])
			if !systemLogTags[tok] {
				username = tok
			}
		}
	}
	return level, username
}

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for len(p) > 0 {
		idx := -1
		for i, c := range p {
			if c == '\n' {
				idx = i
				break
			}
		}

		var line string
		if idx >= 0 {
			line = string(p[:idx])
			p = p[idx+1:]
		} else {
			line = string(p)
			p = nil
		}

		if line != "" {
			if len(b.lines) >= b.cap {
				b.lines = b.lines[1:]
			}
			lvl, user := classifyLog(line)
			b.lines = append(b.lines, logEntry{Time: time.Now(), Level: lvl, Message: line})
			if logSinkCh != nil {
				select {
				case logSinkCh <- parsedLog{level: lvl, username: user, message: line}:
				default:
					// Backpressure: drop rather than block the logging path.
				}
			}
		}
	}

	return len(p), nil
}

func (b *LogBuffer) Lines(n int) []logEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n <= 0 || n > len(b.lines) {
		n = len(b.lines)
	}
	result := make([]logEntry, n)
	copy(result, b.lines[len(b.lines)-n:])
	return result
}

func (b *LogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = b.lines[:0]
}

func (b *LogBuffer) WriteString(s string) {
	b.Write([]byte(s))
}

// LoadWorkflowLogs reads a workflow setup log file (written by the GitHub Actions
// workflow before the keep-alive loop) and injects its lines into the log buffer
// so they appear in /api/logs alongside runtime logs.
func LoadWorkflowLogs(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	buf := GetLogBuffer()
	buf.WriteString("━━━ GitHub Actions workflow setup logs ━━━\n")
	buf.Write(data)
	buf.WriteString("━━━ End of workflow setup logs ━━━\n")
}

func (e logEntry) String() string {
	return fmt.Sprintf("%s %s", e.Time.Format("15:04:05"), e.Message)
}
