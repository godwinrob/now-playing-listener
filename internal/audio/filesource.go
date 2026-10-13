package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"time"
)

// ToneSpec is the NPL_AUDIO_FILE value that means "a built-in test tone" instead of a file.
const ToneSpec = "tone"

// FileSource plays a WAV file (or a test tone) in a loop as the microphone, at the pace a real one
// would record. It lets the listener run, and be developed, on a machine with no audio hardware,
// against a real server.
type FileSource struct {
	pcm   []byte // 16-bit mono
	rate  int
	pos   int // byte offset into pcm, always even
	sleep func(ctx context.Context, d time.Duration) error
}

// NewFileSource reads spec: ToneSpec, or the path of a 16-bit PCM WAV file (stereo is mixed to mono).
func NewFileSource(spec string) (*FileSource, error) {
	var w WAV
	if spec == ToneSpec {
		// Loud enough to be sent (about -20 dBFS), and plainly not music.
		tone, err := ParseWAV(Tone(44100, 10, 0.1))
		if err != nil {
			return nil, err
		}
		w = tone
	} else {
		b, err := os.ReadFile(spec)
		if err != nil {
			return nil, fmt.Errorf("reading audio file: %w", err)
		}
		if w, err = ParseWAV(b); err != nil {
			return nil, fmt.Errorf("%s: %w", spec, err)
		}
	}
	if w.Bits != 16 {
		return nil, fmt.Errorf("%s: only 16-bit audio is supported (this is %d-bit)", spec, w.Bits)
	}
	pcm := w.Data
	if w.Channels == 2 {
		mono := make([]byte, len(pcm)/4*2)
		for i := 0; i+3 < len(pcm); i += 4 {
			l := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
			r := int(int16(binary.LittleEndian.Uint16(pcm[i+2:])))
			binary.LittleEndian.PutUint16(mono[i/2:], uint16(int16((l+r)/2)))
		}
		pcm = mono
	} else if w.Channels != 1 {
		return nil, fmt.Errorf("%s: %d channels is not supported (use mono or stereo)", spec, w.Channels)
	}
	if len(pcm) < 2*w.SampleRate { // under a second of audio
		return nil, fmt.Errorf("%s: needs at least a second of audio", spec)
	}
	return &FileSource{pcm: pcm[:len(pcm)/2*2], rate: w.SampleRate, sleep: sleepFor}, nil
}

// Capture returns the next d of the file as a WAV, wrapping around at the end, after waiting d.
func (f *FileSource) Capture(ctx context.Context, d time.Duration) ([]byte, error) {
	if err := f.sleep(ctx, d); err != nil {
		return nil, err
	}
	n := int(d.Seconds()*float64(f.rate)) * 2
	out := make([]byte, 0, n)
	for len(out) < n {
		take := min(n-len(out), len(f.pcm)-f.pos)
		out = append(out, f.pcm[f.pos:f.pos+take]...)
		f.pos = (f.pos + take) % len(f.pcm)
	}
	return Encode(f.rate, out), nil
}

func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
