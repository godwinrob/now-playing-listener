package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/godwinrob/now-playing/apps/listener/internal/audio"
	"github.com/godwinrob/now-playing/apps/listener/internal/config"
)

type mic struct {
	amp float64
	err error
}

func (m mic) Capture(_ context.Context, d time.Duration) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return audio.Tone(16000, d.Seconds(), m.amp), nil
}

func TestTestMicVerdicts(t *testing.T) {
	cfg := config.Config{AudioDevice: "plughw:1,0", SilenceDBFS: -45}
	for name, c := range map[string]struct {
		amp  float64
		want string
	}{
		"quiet":      {0.001, "Too quiet"},
		"good":       {0.2, "Good"},
		"distorting": {1.0, "Very loud"},
	} {
		var out bytes.Buffer
		if err := testMic(context.Background(), mic{amp: c.amp}, cfg, &out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(out.String(), c.want) || !strings.Contains(out.String(), "plughw:1,0") {
			t.Errorf("%s: output\n%s", name, out.String())
		}
	}
}

func TestTestMicSurfacesCaptureErrors(t *testing.T) {
	var out bytes.Buffer
	err := testMic(context.Background(), mic{err: errors.New("no such device")}, config.Config{SilenceDBFS: -45}, &out)
	if err == nil || !strings.Contains(err.Error(), "no such device") {
		t.Fatalf("%v", err)
	}
}

func TestRunReportsConfigProblems(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"run"}, func(string) string { return "" }, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "NPL_SERVER_URL") || !strings.Contains(errOut.String(), "NPL_DEVICE_TOKEN") {
		t.Fatalf("code=%d\n%s", code, errOut.String())
	}
	if code := run(context.Background(), []string{"bogus"}, nil, &out, &errOut); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
}
