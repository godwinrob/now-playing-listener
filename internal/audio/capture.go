package audio

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Capturer records audio from the microphone.
type Capturer interface {
	// Capture records for d and returns a WAV file.
	Capture(ctx context.Context, d time.Duration) ([]byte, error)
}

// Arecord records with ALSA's arecord, which is on every Raspberry Pi OS install.
type Arecord struct {
	Device     string
	SampleRate int
	// run is replaced in tests.
	run func(ctx context.Context, name string, args ...string) (stdout []byte, stderr string, err error)
}

func NewArecord(device string, sampleRate int) *Arecord {
	return &Arecord{Device: device, SampleRate: sampleRate, run: runCommand}
}

// Args are the arecord arguments for a clip of d: mono 16-bit, to stdout.
func (a *Arecord) Args(d time.Duration) []string {
	secs := int(d.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return []string{"-q", "-D", a.Device, "-f", "S16_LE", "-r", strconv.Itoa(a.SampleRate), "-c", "1", "-d", strconv.Itoa(secs), "-t", "wav", "-"}
}

func (a *Arecord) Capture(ctx context.Context, d time.Duration) ([]byte, error) {
	// arecord stops itself after -d; the timeout only guards against a wedged device.
	ctx, cancel := context.WithTimeout(ctx, d+15*time.Second)
	defer cancel()
	out, stderr, err := a.run(ctx, "arecord", a.Args(d)...)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("arecord on %q failed: %w: %s", a.Device, err, msg)
	}
	if len(out) < 44 {
		return nil, fmt.Errorf("arecord on %q returned no audio", a.Device)
	}
	return out, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.Bytes(), errOut.String(), err
}
