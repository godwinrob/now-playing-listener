package server

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUploadSendsTokenAndClip(t *testing.T) {
	var gotAuth, gotRecorded string
	var gotAudio []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/listen" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			switch p.FormName() {
			case "audio":
				gotAudio = b
			case "recordedAt":
				gotRecorded = string(b)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"mode":"tracking","recognition":{"id":"x","receivedAt":"2026-10-07T20:00:00Z","outcome":"matched","matchedTitle":"Abbey Road"}}`)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "secret-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	res, err := c.Upload(context.Background(), []byte("RIFFdata"), at)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "tracking" || res.Outcome != "matched" || res.Title != "Abbey Road" {
		t.Fatalf("%+v", res)
	}
	if gotAuth != "Bearer secret-token" || string(gotAudio) != "RIFFdata" || gotRecorded != "2026-10-07T20:00:00Z" {
		t.Fatalf("auth=%q audio=%q recorded=%q", gotAuth, gotAudio, gotRecorded)
	}
}

func TestUnauthorizedAndServerErrors(t *testing.T) {
	status := http.StatusUnauthorized
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, `{"error":{"code":"x","message":"nope"}}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "t", nil)
	ctx := context.Background()

	if _, err := c.Upload(ctx, []byte("x"), time.Now()); err != ErrUnauthorized {
		t.Fatalf("upload: %v", err)
	}
	if err := c.Heartbeat(ctx, -30, "searching"); err != ErrUnauthorized {
		t.Fatalf("heartbeat: %v", err)
	}
	status = http.StatusBadRequest
	if _, err := c.Upload(ctx, []byte("x"), time.Now()); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("400 should carry the server's message: %v", err)
	}
	status = http.StatusInternalServerError
	if err := c.Heartbeat(ctx, -30, "searching"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("%v", err)
	}
}

func TestHeartbeatBody(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "t", nil)
	if err := c.Heartbeat(context.Background(), -31.5, "tracking"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"level":-31.5`) || !strings.Contains(body, `"mode":"tracking"`) {
		t.Fatalf("body %s", body)
	}
}

func TestSignOffAndServerMessages(t *testing.T) {
	var gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth = r.Method, r.Header.Get("Authorization")
		if r.URL.Path != "/api/listener/heartbeat" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "npk_abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SignOff(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotAuth != "Bearer npk_abc" {
		t.Errorf("sign-off sent %s with %q", gotMethod, gotAuth)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"not_wav","message":"The clip must be a WAV file."}}`))
	}))
	defer bad.Close()
	c2, _ := New(bad.URL, "k", nil)
	_, err = c2.Upload(context.Background(), []byte("x"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "The clip must be a WAV file.") {
		t.Errorf("want the server's message, got %v", err)
	}
}
