// Package agent is the listener's loop: record clips, skip silence, send sound to the server, and
// check less often once a record has been recognized.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/godwinrob/now-playing-listener/internal/audio"
	"github.com/godwinrob/now-playing-listener/internal/server"
)

const (
	ModeSearching = "searching"
	ModeTracking  = "tracking"

	probeLength = 2 * time.Second // a quick level check between clips, so heartbeats stay fresh
	minBackoff  = 5 * time.Second
	maxBackoff  = 2 * time.Minute
	minSleep    = time.Second
)

// Uploader is the server as the agent sees it.
type Uploader interface {
	Upload(ctx context.Context, wav []byte, recordedAt time.Time) (server.Result, error)
	Heartbeat(ctx context.Context, levelDBFS float64, mode string) error
}

// Clock lets tests run hours of listening instantly.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type Settings struct {
	ClipLength        time.Duration
	SearchInterval    time.Duration
	TrackInterval     time.Duration
	HeartbeatInterval time.Duration
	SilenceDBFS       float64
}

type Agent struct {
	set   Settings
	mic   audio.Capturer
	srv   Uploader
	clock Clock
	log   *slog.Logger

	mode        string
	level       float64
	lastClip    time.Time
	lastBeat    time.Time
	micFails    int // consecutive recording failures
	uploadFails int // consecutive upload failures
	beatBroke   bool
}

func New(set Settings, mic audio.Capturer, srv Uploader, clock Clock, log *slog.Logger) *Agent {
	if log == nil {
		log = slog.Default()
	}
	return &Agent{set: set, mic: mic, srv: srv, clock: clock, log: log, mode: ModeSearching, level: audio.FloorDBFS}
}

// Mode is what the agent believes the server is doing: searching for a record or tracking one.
func (a *Agent) Mode() string { return a.mode }

// Run listens until ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		wait := a.Step(ctx)
		if err := a.clock.Sleep(ctx, wait); err != nil {
			return nil
		}
	}
	return nil
}

func (a *Agent) interval() time.Duration {
	if a.mode == ModeTracking {
		return a.set.TrackInterval
	}
	return a.set.SearchInterval
}

// Step does whatever is due (a clip, or a quick level check for the heartbeat) and returns how long to wait.
func (a *Agent) Step(ctx context.Context) time.Duration {
	now := a.clock.Now()
	switch {
	case a.lastClip.IsZero() || now.Sub(a.lastClip) >= a.interval():
		a.clip(ctx)
	case now.Sub(a.lastBeat) >= a.set.HeartbeatInterval:
		a.probe(ctx)
	}
	return a.untilNext()
}

func backoff(failures int) time.Duration {
	d := minBackoff
	for i := 1; i < failures && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

func (a *Agent) untilNext() time.Duration {
	now := a.clock.Now()
	next := a.lastClip.Add(a.interval())
	if beat := a.lastBeat.Add(a.set.HeartbeatInterval); beat.Before(next) {
		next = beat
	}
	return max(next.Sub(now), minSleep)
}

// retryClipIn makes the next clip due after d instead of after the normal interval.
func (a *Agent) retryClipIn(d time.Duration) {
	a.lastClip = a.clock.Now().Add(d - a.interval())
}

func (a *Agent) clip(ctx context.Context) {
	started := a.clock.Now()
	wav, err := a.mic.Capture(ctx, a.set.ClipLength)
	if ctx.Err() != nil {
		return
	}
	var level float64
	if err == nil {
		level, err = audio.Level(wav)
	}
	if err != nil {
		a.micFails++
		a.failed("recording failed", err, a.micFails)
		a.retryClipIn(backoff(a.micFails))
		a.level = audio.FloorDBFS // it can't hear anything
		a.beat(ctx)
		return
	}
	a.micFails = 0
	a.lastClip, a.level = started, level

	if level <= a.set.SilenceDBFS {
		a.log.Debug("room is quiet, not uploading", "level_dbfs", level)
		a.mode = ModeSearching
		a.uploadFails = 0
		a.beat(ctx)
		return
	}
	res, err := a.srv.Upload(ctx, wav, started)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		a.uploadFails++
		a.failed("upload failed", err, a.uploadFails)
		a.retryClipIn(backoff(a.uploadFails))
		a.beat(ctx)
		return
	}
	a.uploadFails = 0
	if res.Mode == ModeTracking || res.Mode == ModeSearching {
		a.mode = res.Mode
	}
	a.log.Info("clip sent", "level_dbfs", round(level), "outcome", res.Outcome, "record", res.Title, "mode", a.mode)
	a.beat(ctx)
}

func (a *Agent) probe(ctx context.Context) {
	wav, err := a.mic.Capture(ctx, probeLength)
	if ctx.Err() != nil {
		return
	}
	var level float64
	if err == nil {
		level, err = audio.Level(wav)
	}
	if err != nil {
		a.micFails++
		a.failed("level check failed", err, a.micFails)
		a.level = audio.FloorDBFS
		a.beat(ctx)
		return
	}
	a.micFails = 0
	a.level = level
	a.beat(ctx)
}

func (a *Agent) beat(ctx context.Context) {
	a.lastBeat = a.clock.Now()
	err := a.srv.Heartbeat(ctx, a.level, a.mode)
	switch {
	case err == nil && a.beatBroke:
		a.beatBroke = false
		a.log.Info("heartbeat is working again")
	case err != nil && ctx.Err() == nil && !a.beatBroke:
		a.beatBroke = true // log the first failure and the recovery, not every 30 seconds in between
		a.log.Warn("heartbeat failed", "err", err)
	}
}

func (a *Agent) failed(what string, err error, failures int) {
	level := slog.LevelWarn
	if errors.Is(err, server.ErrUnauthorized) {
		level = slog.LevelError
	}
	a.log.Log(context.Background(), level, what, "err", err, "failures", failures, "retry_in", backoff(failures))
	// The room's state is unknown now; don't claim to be following a record.
	a.mode = ModeSearching
}

func round(f float64) float64 { return float64(int(f*10)) / 10 }
