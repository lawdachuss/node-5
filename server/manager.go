package server

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/teacat/chaturbate-dvr/entity"
)

var Manager IManager

// shuttingDown is set when the process receives SIGTERM/SIGINT so that
// in-flight and queued uploads are skipped during graceful shutdown.
var shuttingDown atomic.Bool

// SetShuttingDown marks the process as shutting down (uploads are skipped).
func SetShuttingDown() { shuttingDown.Store(true) }

// IsShuttingDown reports whether the process is shutting down.
func IsShuttingDown() bool { return shuttingDown.Load() }

type IManager interface {
	CreateChannel(conf *entity.ChannelConfig, shouldSave bool) error
	StopChannel(username string) error
	PauseChannel(username string) error
	ResumeChannel(username string) error
	ChannelInfo() []*entity.ChannelInfo
	Publish(name string, ch *entity.ChannelInfo)
	PublishLog(username, line string)
	PublishUploadState()
	Subscriber(w http.ResponseWriter, r *http.Request)
	LoadConfig() error
	SaveConfig() error
	WaitForUploads()
	StopAllChannels()
	WaitForAllChannels()
	StartSession(duration time.Duration)
	IsFileUploadInFlight(filePath string) bool
	SessionInfo() (time.Duration, bool)
	TriggerSessionStop()
	StopSession()
	UploadEntries() *entity.UploadsResponse
}
