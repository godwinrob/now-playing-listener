// Command listener runs on the Raspberry Pi next to the turntable. It records from the USB
// microphone and sends what it hears to the Now Playing server.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/godwinrob/now-playing/apps/listener/internal/agent"
	"github.com/godwinrob/now-playing/apps/listener/internal/audio"
	"github.com/godwinrob/now-playing/apps/listener/internal/config"
	"github.com/godwinrob/now-playing/apps/listener/internal/server"
)

// Set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `Usage: listener [command]

Commands:
  run         Listen and send clips to the server (default)
  test-mic    Show the microphone level for a few seconds, to check it works
  version     Print the version

Settings come from NPL_* environment variables; see deploy/pi/listener.env.example.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
	}
	var err error
	switch cmd {
	case "run":
		err = listen(ctx, getenv, stderr)
	case "test-mic":
		err = testMic(ctx, audio.NewArecord(config.LoadMic(getenv).AudioDevice, config.LoadMic(getenv).SampleRate), config.LoadMic(getenv), stdout)
	case "version":
		fmt.Fprintf(stdout, "listener %s\n", version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "listener: %v\n", err)
		return 1
	}
	return 0
}

func listen(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("configuration problems:\n%w", err)
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	log.Info("starting", "version", version, "config", cfg)

	srv, err := server.New(cfg.ServerURL, cfg.DeviceToken, nil)
	if err != nil {
		return err
	}
	a := agent.New(agent.Settings{
		ClipLength:        time.Duration(cfg.ClipSeconds) * time.Second,
		SearchInterval:    cfg.SearchInterval,
		TrackInterval:     cfg.TrackInterval,
		HeartbeatInterval: cfg.HeartbeatInterval,
		SilenceDBFS:       cfg.SilenceDBFS,
	}, audio.NewArecord(cfg.AudioDevice, cfg.SampleRate), srv, agent.RealClock{}, log)
	if err := a.Run(ctx); err != nil {
		return err
	}
	log.Info("stopped")
	return nil
}

// testMic records one-second samples and draws a level meter, so a missing or muted microphone
// shows up in the first minute instead of after an evening of silence.
func testMic(ctx context.Context, mic audio.Capturer, cfg config.Config, out io.Writer) error {
	fmt.Fprintf(out, "Recording from %q. Play a record or talk near the microphone.\n\n", cfg.AudioDevice)
	var loudest float64 = audio.FloorDBFS
	for i := 0; i < 8; i++ {
		clip, err := mic.Capture(ctx, time.Second)
		if err != nil {
			return err
		}
		level, err := audio.Level(clip)
		if err != nil {
			return err
		}
		loudest = max(loudest, level)
		fmt.Fprintf(out, "%6.1f dBFS  %s\n", level, meter(level))
	}
	fmt.Fprintln(out)
	switch {
	case loudest <= cfg.SilenceDBFS:
		fmt.Fprintf(out, "Too quiet: the loudest sample (%.1f dBFS) is below the silence threshold (%.0f dBFS), so nothing would be sent.\n"+
			"Check the microphone is plugged in and not muted (alsamixer), the device name (arecord -l), and the volume of the record.\n", loudest, cfg.SilenceDBFS)
	case loudest > -6:
		fmt.Fprintf(out, "Very loud (%.1f dBFS): the clip may be distorted. Lower the capture volume in alsamixer.\n", loudest)
	default:
		fmt.Fprintf(out, "Good: the loudest sample was %.1f dBFS, above the silence threshold of %.0f dBFS.\n", loudest, cfg.SilenceDBFS)
	}
	return nil
}

func meter(level float64) string {
	n := int((level - audio.FloorDBFS) / 3) // 40 characters from -120 to 0 dBFS
	return strings.Repeat("█", max(n, 0))
}
