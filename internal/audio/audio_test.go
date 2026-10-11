package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func near(t *testing.T, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("got %.2f, want %.2f ± %.2f", got, want, tol)
	}
}

func TestLevels(t *testing.T) {
	// A sine of amplitude A has RMS A/sqrt(2): full scale is -3 dBFS, 0.1 is -23 dBFS.
	for _, c := range []struct{ amp, want float64 }{{1, -3.01}, {0.1, -23.01}, {0.01, -43.01}} {
		l, err := Level(Tone(16000, 1, c.amp))
		if err != nil {
			t.Fatal(err)
		}
		near(t, l, c.want, 0.2)
	}
	l, _ := Level(Tone(16000, 1, 0))
	if l != FloorDBFS {
		t.Fatalf("silence = %v", l)
	}
}

func TestParseWAVStreamingHeader(t *testing.T) {
	// arecord writing to a pipe leaves the sizes unset.
	clip := Tone(16000, 1, 0.5)
	binary.LittleEndian.PutUint32(clip[4:], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(clip[40:], 0xFFFFFFFF)
	w, err := ParseWAV(clip)
	if err != nil {
		t.Fatal(err)
	}
	if w.SampleRate != 16000 || w.Channels != 1 || w.Bits != 16 || len(w.Data) != 32000 {
		t.Fatalf("%+v len=%d", w, len(w.Data))
	}
	binary.LittleEndian.PutUint32(clip[40:], 0)
	if w, err = ParseWAV(clip); err != nil || len(w.Data) != 32000 {
		t.Fatalf("zero size: %v len=%d", err, len(w.Data))
	}
}

func TestParseWAVSkipsOtherChunks(t *testing.T) {
	clip := Tone(8000, 0.5, 0.5)
	list := append([]byte("LIST\x05\x00\x00\x00hello"), 0) // odd size is padded
	withList := append(append(append([]byte{}, clip[:36]...), list...), clip[36:]...)
	w, err := ParseWAV(withList)
	if err != nil || len(w.Data) != 8000 {
		t.Fatalf("%v len=%d", err, len(w.Data))
	}
}

func TestParseWAVRejectsJunk(t *testing.T) {
	for name, b := range map[string][]byte{"empty": nil, "text": []byte("hello world, not audio"), "header only": Tone(8000, 0, 0)[:20]} {
		if _, err := ParseWAV(b); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	stereo := Tone(8000, 0.1, 0.5)
	stereo[34] = 8 // 8-bit
	if _, err := Level(stereo); err == nil {
		t.Error("8-bit audio should be rejected")
	}
}

func TestArecordArgsAndErrors(t *testing.T) {
	a := NewArecord("plughw:1,0", 16000)
	got := strings.Join(a.Args(10*time.Second), " ")
	if got != "-q -D plughw:1,0 -f S16_LE -r 16000 -c 1 -d 10 -t wav -" {
		t.Fatalf("args: %s", got)
	}
	if !strings.Contains(strings.Join(a.Args(100*time.Millisecond), " "), "-d 1 ") {
		t.Fatal("sub-second clips should round up to one second")
	}

	a.run = func(context.Context, string, ...string) ([]byte, string, error) {
		return nil, "arecord: main:831: audio open error: Device or resource busy", errors.New("exit status 1")
	}
	if _, err := a.Capture(context.Background(), time.Second); err == nil || !strings.Contains(err.Error(), "Device or resource busy") {
		t.Fatalf("error should carry arecord's reason: %v", err)
	}
	a.run = func(context.Context, string, ...string) ([]byte, string, error) { return []byte("x"), "", nil }
	if _, err := a.Capture(context.Background(), time.Second); err == nil {
		t.Fatal("empty output should be an error")
	}
	want := Tone(16000, 1, 0.3)
	a.run = func(context.Context, string, ...string) ([]byte, string, error) { return want, "", nil }
	if got, err := a.Capture(context.Background(), time.Second); err != nil || len(got) != len(want) {
		t.Fatalf("%v", err)
	}
}
