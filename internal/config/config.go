// Package config reads the listener's NPL_* environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerURL         string
	DeviceToken       string
	AudioDevice       string
	SampleRate        int
	ClipSeconds       int
	SearchInterval    time.Duration // between clips while looking for a record
	TrackInterval     time.Duration // between clips once a match is confirmed
	HeartbeatInterval time.Duration
	SilenceDBFS       float64
	LogLevel          slog.Level
}

// Load reads settings through getenv so tests can pass a map. It reports every problem at once.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	fail := func(name, msg string) { errs = append(errs, fmt.Errorf("%s: %s", name, msg)) }
	str := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return def
	}
	integer := func(name string, def, min, max int) int {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return def
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < min || n > max {
			fail(name, fmt.Sprintf("must be a whole number from %d to %d", min, max))
			return def
		}
		return n
	}
	dur := func(name string, def, min time.Duration) time.Duration {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return def
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d < min {
			fail(name, fmt.Sprintf("must be a duration of at least %s, such as 30s or 2m", min))
			return def
		}
		return d
	}

	c := Config{
		AudioDevice:       str("NPL_ALSA_DEVICE", "default"),
		SampleRate:        integer("NPL_SAMPLE_RATE", 44100, 8000, 48000),
		ClipSeconds:       integer("NPL_CLIP_SECONDS", 10, 5, 30),
		SearchInterval:    dur("NPL_SEARCH_INTERVAL", 30*time.Second, 5*time.Second),
		TrackInterval:     dur("NPL_TRACK_INTERVAL", 3*time.Minute, 10*time.Second),
		HeartbeatInterval: dur("NPL_HEARTBEAT_INTERVAL", 30*time.Second, 5*time.Second),
		SilenceDBFS:       -45,
	}

	c.ServerURL = strings.TrimRight(strings.TrimSpace(getenv("NPL_SERVER_URL")), "/")
	if c.ServerURL == "" {
		fail("NPL_SERVER_URL", "is required, e.g. http://tower.local:8080")
	} else if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fail("NPL_SERVER_URL", "must be a full http:// or https:// URL")
	}
	c.DeviceToken = strings.TrimSpace(getenv("NPL_DEVICE_TOKEN"))
	if c.DeviceToken == "" {
		fail("NPL_DEVICE_TOKEN", "is required (the same value as NP_DEVICE_TOKEN on the server)")
	}
	if raw := strings.TrimSpace(getenv("NPL_SILENCE_DBFS")); raw != "" {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f > -10 || f < -90 {
			fail("NPL_SILENCE_DBFS", "must be a number from -90 to -10 (default -45)")
		} else {
			c.SilenceDBFS = f
		}
	}
	if err := c.LogLevel.UnmarshalText([]byte(str("NPL_LOG_LEVEL", "info"))); err != nil {
		fail("NPL_LOG_LEVEL", "must be debug, info, warn or error")
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

// LogValue prints the token as [redacted].
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("server_url", c.ServerURL),
		slog.String("device_token", "[redacted]"),
		slog.String("audio_device", c.AudioDevice),
		slog.Int("sample_rate", c.SampleRate),
		slog.Int("clip_seconds", c.ClipSeconds),
		slog.Duration("search_interval", c.SearchInterval),
		slog.Duration("track_interval", c.TrackInterval),
		slog.Duration("heartbeat_interval", c.HeartbeatInterval),
		slog.Float64("silence_dbfs", c.SilenceDBFS),
	)
}

// LoadMic reads only the microphone settings, for `listener test-mic` before the server is set up.
func LoadMic(getenv func(string) string) Config {
	c := Config{AudioDevice: "default", SampleRate: 44100, SilenceDBFS: -45}
	if v := strings.TrimSpace(getenv("NPL_ALSA_DEVICE")); v != "" {
		c.AudioDevice = v
	}
	if n, err := strconv.Atoi(strings.TrimSpace(getenv("NPL_SAMPLE_RATE"))); err == nil && n >= 8000 && n <= 48000 {
		c.SampleRate = n
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(getenv("NPL_SILENCE_DBFS")), 64); err == nil && f <= -10 && f >= -90 {
		c.SilenceDBFS = f
	}
	return c
}
