package audio

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func instant(context.Context, time.Duration) error { return nil }

func TestToneSourceIsLoudEnoughToBeSentAndLoops(t *testing.T) {
	src, err := NewFileSource(ToneSpec)
	if err != nil {
		t.Fatal(err)
	}
	src.sleep = instant
	// 25 s from a 10 s tone: wraps twice.
	clip, err := src.Capture(context.Background(), 25*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	w, err := ParseWAV(clip)
	if err != nil || w.SampleRate != 44100 || w.Channels != 1 || len(w.Data) != 25*44100*2 {
		t.Fatalf("%+v %v", w, err)
	}
	level, _ := w.LevelDBFS()
	if level < -25 || level > -15 {
		t.Errorf("level = %.1f dBFS, want about -20 (above the -45 silence threshold)", level)
	}
}

func TestFileSourcePlaysTheFileInOrderAndMixesStereoToMono(t *testing.T) {
	// One second at 8 kHz, stereo: left is the sample index, right is the same plus 10.
	const rate = 8000
	data := make([]byte, rate*4)
	for i := 0; i < rate; i++ {
		binary.LittleEndian.PutUint16(data[4*i:], uint16(int16(i)))
		binary.LittleEndian.PutUint16(data[4*i+2:], uint16(int16(i+10)))
	}
	h := Encode(rate, data) // header says mono; patch it to stereo
	binary.LittleEndian.PutUint16(h[22:], 2)
	path := filepath.Join(t.TempDir(), "in.wav")
	if err := os.WriteFile(path, h, 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := NewFileSource(path)
	if err != nil {
		t.Fatal(err)
	}
	src.sleep = instant
	clip, _ := src.Capture(context.Background(), 500*time.Millisecond)
	w, _ := ParseWAV(clip)
	if w.SampleRate != rate || len(w.Data) != rate/2*2 {
		t.Fatalf("%+v", w)
	}
	for _, i := range []int{0, 100, 3999} {
		if got := int16(binary.LittleEndian.Uint16(w.Data[2*i:])); got != int16(i+5) {
			t.Errorf("sample %d = %d, want %d (the mix of both channels)", i, got, i+5)
		}
	}
	// The next clip carries on where the last stopped.
	next, _ := src.Capture(context.Background(), 500*time.Millisecond)
	w2, _ := ParseWAV(next)
	if got := int16(binary.LittleEndian.Uint16(w2.Data)); got != int16(4000+5) {
		t.Errorf("second clip starts at %d, want 4005", got)
	}
}

func TestFileSourceRefusesWhatItCannotPlay(t *testing.T) {
	if _, err := NewFileSource(filepath.Join(t.TempDir(), "missing.wav")); err == nil {
		t.Error("a missing file should be an error")
	}
	short := filepath.Join(t.TempDir(), "short.wav")
	_ = os.WriteFile(short, Encode(8000, make([]byte, 100)), 0o644)
	if _, err := NewFileSource(short); err == nil {
		t.Error("under a second of audio should be an error")
	}
}

func TestFileSourceStopsWithItsContext(t *testing.T) {
	src, _ := NewFileSource(ToneSpec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Capture(ctx, time.Hour); err == nil {
		t.Error("Capture should return when the context ends")
	}
}
