package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/godwinrob/now-playing-listener/internal/audio"
	"github.com/godwinrob/now-playing-listener/internal/server"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return nil
}

type capture struct {
	d     time.Duration
	level float64 // amplitude 0..1
}

type fakeMic struct {
	clock    *fakeClock
	amp      float64
	err      error
	captures []capture
}

func (m *fakeMic) Capture(_ context.Context, d time.Duration) ([]byte, error) {
	m.captures = append(m.captures, capture{d: d, level: m.amp})
	m.clock.now = m.clock.now.Add(d) // recording takes as long as the clip
	if m.err != nil {
		return nil, m.err
	}
	return audio.Tone(16000, d.Seconds(), m.amp), nil
}

type beat struct {
	level float64
	mode  string
}

type fakeServer struct {
	clock     *fakeClock
	attempts  []time.Time
	uploads   int
	uploadErr error
	reply     server.Result
	beats     []beat
	beatErr   error
}

func (s *fakeServer) Upload(context.Context, []byte, time.Time) (server.Result, error) {
	s.uploads++
	if s.clock != nil {
		s.attempts = append(s.attempts, s.clock.now)
	}
	if s.uploadErr != nil {
		return server.Result{}, s.uploadErr
	}
	return s.reply, nil
}

func (s *fakeServer) Heartbeat(_ context.Context, level float64, mode string) error {
	s.beats = append(s.beats, beat{level, mode})
	return s.beatErr
}

var settings = Settings{ClipLength: 10 * time.Second, SearchInterval: 20 * time.Second, TrackInterval: 2 * time.Minute, HeartbeatInterval: 30 * time.Second, SilenceDBFS: -45}

func newAgent(amp float64) (*Agent, *fakeMic, *fakeServer, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)}
	mic := &fakeMic{clock: clock, amp: amp}
	srv := &fakeServer{clock: clock, reply: server.Result{Mode: ModeSearching, Outcome: "unmatched"}}
	return New(settings, mic, srv, clock, nil), mic, srv, clock
}

// run drives the agent for the given stretch of fake time.
func run(a *Agent, clock *fakeClock, d time.Duration) {
	end := clock.now.Add(d)
	for clock.now.Before(end) {
		clock.now = clock.now.Add(a.Step(context.Background()))
	}
}

func TestSilentRoomIsNeverUploaded(t *testing.T) {
	a, mic, srv, clock := newAgent(0.0005) // about -69 dBFS
	run(a, clock, 5*time.Minute)
	if srv.uploads != 0 {
		t.Fatalf("uploaded %d clips of silence", srv.uploads)
	}
	if len(mic.captures) == 0 || len(srv.beats) == 0 {
		t.Fatal("it should keep listening and keep sending heartbeats")
	}
	for _, b := range srv.beats {
		if b.level > -45 || b.mode != ModeSearching {
			t.Fatalf("bad heartbeat %+v", b)
		}
	}
}

func TestSoundIsUploadedAtTheSearchRate(t *testing.T) {
	a, _, srv, clock := newAgent(0.2)
	run(a, clock, 2*time.Minute)
	// A 10 s clip every 20 s for 2 minutes is about 6 uploads.
	if srv.uploads < 5 || srv.uploads > 7 {
		t.Fatalf("uploads = %d", srv.uploads)
	}
}

func TestTrackingChecksLessOftenAndKeepsHeartbeatsFresh(t *testing.T) {
	a, mic, srv, clock := newAgent(0.2)
	srv.reply = server.Result{Mode: ModeTracking, Outcome: "matched", Title: "Abbey Road"}
	run(a, clock, 10*time.Minute)

	if a.Mode() != ModeTracking {
		t.Fatalf("mode = %s", a.Mode())
	}
	// First clip at once, then one every 2 minutes: 5 or 6 in ten minutes (not 30).
	if srv.uploads < 5 || srv.uploads > 6 {
		t.Fatalf("uploads = %d", srv.uploads)
	}
	probes := 0
	for _, c := range mic.captures {
		if c.d == probeLength {
			probes++
		}
	}
	if probes < 10 {
		t.Fatalf("only %d level checks between clips", probes)
	}
	// About one heartbeat every 30 s.
	if len(srv.beats) < 18 || len(srv.beats) > 24 {
		t.Fatalf("heartbeats = %d", len(srv.beats))
	}
	if last := srv.beats[len(srv.beats)-1]; last.mode != ModeTracking {
		t.Fatalf("last heartbeat %+v", last)
	}
}

func TestRecordEndingReturnsToSearching(t *testing.T) {
	a, mic, srv, clock := newAgent(0.2)
	srv.reply = server.Result{Mode: ModeTracking, Outcome: "matched"}
	run(a, clock, 3*time.Minute)
	if a.Mode() != ModeTracking {
		t.Fatal("should be tracking")
	}
	mic.amp = 0 // the record ends
	run(a, clock, 3*time.Minute)
	if a.Mode() != ModeSearching {
		t.Fatal("silence should put the agent back to searching")
	}
	last := srv.beats[len(srv.beats)-1]
	if last.mode != ModeSearching || last.level > -100 {
		t.Fatalf("last heartbeat %+v", last)
	}
	// And a new record is picked up at the quicker search rate.
	uploads := srv.uploads
	mic.amp = 0.2
	srv.reply = server.Result{Mode: ModeSearching, Outcome: "unmatched"}
	run(a, clock, time.Minute)
	if srv.uploads-uploads < 2 {
		t.Fatalf("new uploads = %d", srv.uploads-uploads)
	}
}

func TestUploadFailuresBackOffThenRecover(t *testing.T) {
	a, _, srv, clock := newAgent(0.2)
	srv.uploadErr = errors.New("connection refused")
	run(a, clock, 10*time.Minute)

	// Each retry waits twice as long as the last (5 s, 10 s, 20 s ... capped at 2 min), plus the 10 s it takes to record.
	want := []time.Duration{15 * time.Second, 20 * time.Second, 30 * time.Second, 50 * time.Second, 90 * time.Second, 130 * time.Second, 130 * time.Second}
	if len(srv.attempts) < len(want)+1 {
		t.Fatalf("only %d attempts", len(srv.attempts))
	}
	for i, w := range want {
		if got := srv.attempts[i+1].Sub(srv.attempts[i]); got != w {
			t.Fatalf("gap %d = %v, want %v (attempts %v)", i, got, w, srv.attempts)
		}
	}
	if len(srv.beats) == 0 {
		t.Fatal("heartbeats should still be tried so the server can see the Pi is alive")
	}

	srv.uploadErr = nil
	srv.reply = server.Result{Mode: ModeTracking}
	run(a, clock, 3*time.Minute)
	if a.uploadFails != 0 || a.Mode() != ModeTracking {
		t.Fatalf("failures=%d mode=%s", a.uploadFails, a.Mode())
	}
}

func TestMicFailureBacksOffAndKeepsTrying(t *testing.T) {
	a, mic, srv, clock := newAgent(0.2)
	mic.err = errors.New("device busy")
	w1 := a.Step(context.Background())
	clock.now = clock.now.Add(w1)
	w2 := a.Step(context.Background())
	if w1 != 5*time.Second || w2 != 10*time.Second || srv.uploads != 0 {
		t.Fatalf("w1=%v w2=%v uploads=%d", w1, w2, srv.uploads)
	}
	mic.err = nil
	clock.now = clock.now.Add(w2)
	a.Step(context.Background())
	if srv.uploads != 1 || a.micFails != 0 {
		t.Fatalf("uploads=%d failures=%d", srv.uploads, a.micFails)
	}
}

func TestRejectedTokenIsReportedAsAnError(t *testing.T) {
	a, _, srv, _ := newAgent(0.2)
	srv.uploadErr = server.ErrUnauthorized
	a.Step(context.Background())
	if a.uploadFails != 1 {
		t.Fatal("should count as a failure")
	}
}

func TestRunStopsWhenContextEnds(t *testing.T) {
	a, _, _, clock := newAgent(0.2)
	ctx, cancel := context.WithCancel(context.Background())
	steps := 0
	a.clock = stopAfter{fakeClock: clock, n: 3, steps: &steps, cancel: cancel}
	if err := a.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if steps != 3 {
		t.Fatalf("steps = %d", steps)
	}
}

type stopAfter struct {
	*fakeClock
	n      int
	steps  *int
	cancel context.CancelFunc
}

func (s stopAfter) Sleep(ctx context.Context, d time.Duration) error {
	*s.steps++
	if *s.steps >= s.n {
		s.cancel()
		return ctx.Err()
	}
	return s.fakeClock.Sleep(ctx, d)
}
