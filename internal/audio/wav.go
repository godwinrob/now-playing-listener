// Package audio reads the WAV clips arecord produces and measures how loud they are.
package audio

import (
	"encoding/binary"
	"errors"
	"math"
)

// FloorDBFS is what silence reads as. JSON cannot carry -Inf, and nothing quieter matters.
const FloorDBFS = -120.0

// WAV is a PCM clip.
type WAV struct {
	SampleRate int
	Channels   int
	Bits       int
	Data       []byte
}

// ParseWAV reads the header of a RIFF/WAVE file and returns its PCM data.
// arecord writing to a pipe cannot go back and fix the chunk sizes, so it may leave them as 0 or
// 0xFFFFFFFF; whatever follows the data chunk header is treated as samples.
func ParseWAV(b []byte) (WAV, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return WAV{}, errors.New("not a WAV file")
	}
	var w WAV
	haveFmt := false
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := pos + 8
		switch id {
		case "fmt ":
			if body+16 > len(b) {
				return WAV{}, errors.New("truncated fmt chunk")
			}
			if tag := binary.LittleEndian.Uint16(b[body:]); tag != 1 && tag != 0xFFFE {
				return WAV{}, errors.New("only PCM audio is supported")
			}
			w.Channels = int(binary.LittleEndian.Uint16(b[body+2:]))
			w.SampleRate = int(binary.LittleEndian.Uint32(b[body+4:]))
			w.Bits = int(binary.LittleEndian.Uint16(b[body+14:]))
			haveFmt = true
		case "data":
			if !haveFmt {
				return WAV{}, errors.New("data chunk before fmt chunk")
			}
			end := body + size
			if size == 0 || end > len(b) || end < body {
				end = len(b)
			}
			w.Data = b[body:end]
			return w, nil
		}
		next := body + size + size%2
		if size < 0 || next <= pos || next > len(b) {
			break
		}
		pos = next
	}
	return WAV{}, errors.New("no data chunk")
}

// LevelDBFS is the RMS level of 16-bit PCM in decibels relative to full scale (0 is the loudest
// possible steady signal, a full-scale square wave). Empty or silent audio gives FloorDBFS.
func (w WAV) LevelDBFS() (float64, error) {
	if w.Bits != 16 {
		return 0, errors.New("only 16-bit audio is supported")
	}
	n := len(w.Data) / 2
	if n == 0 {
		return FloorDBFS, nil
	}
	var sum float64
	for i := 0; i < n; i++ {
		s := float64(int16(binary.LittleEndian.Uint16(w.Data[2*i:]))) / 32768
		sum += s * s
	}
	rms := math.Sqrt(sum / float64(n))
	if rms == 0 {
		return FloorDBFS, nil
	}
	return math.Max(FloorDBFS, math.Min(0, 20*math.Log10(rms))), nil
}

// Level parses a clip and returns its level.
func Level(clip []byte) (float64, error) {
	w, err := ParseWAV(clip)
	if err != nil {
		return 0, err
	}
	return w.LevelDBFS()
}

// Tone builds a mono 16-bit WAV of a sine wave. amplitude is 0 to 1; 0 gives silence.
// It exists for tests and for `listener test-mic --self-test`.
func Tone(sampleRate int, seconds float64, amplitude float64) []byte {
	n := int(float64(sampleRate) * seconds)
	data := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		v := int16(amplitude * 32767 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
		binary.LittleEndian.PutUint16(data[2*i:], uint16(v))
	}
	return Encode(sampleRate, data)
}

// Encode wraps 16-bit mono PCM in a WAV header.
func Encode(sampleRate int, pcm []byte) []byte {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(pcm)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(h[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(pcm)))
	return append(h, pcm...)
}
