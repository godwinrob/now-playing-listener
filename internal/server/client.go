// Package server talks to the Now Playing server. The listener needs three calls (upload a clip,
// heartbeat, sign off), so this is plain net/http rather than a client generated from the server's
// OpenAPI contract: it keeps this repo from depending on the server's.
//
//	POST /api/listener/clips      multipart "audio" (WAV) and "recordedAt"; returns the recognition and mode
//	POST /api/listener/heartbeat  JSON {"level": dBFS, "mode": "searching"|"tracking"}; 204
//	POST /api/listener/sign-off   the microphone is off; 204
//
// Every call carries "Authorization: Bearer <API key>". Make the key on the server's Host page.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized means the server rejected the API key.
var ErrUnauthorized = errors.New("the server rejected the API key (check NPL_API_KEY; if it was revoked, make a new one on the Host page)")

// Result is what the server made of a clip.
type Result struct {
	Mode    string // "searching" or "tracking": what to do next
	Outcome string // matched, unmatched, no_result or error
	Title   string // the matched record, when there is one
}

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("server address %q must be a full http:// or https:// URL", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	return &Client{base: baseURL, token: token, http: httpClient}, nil
}

// Upload sends a WAV clip for recognition.
func (c *Client) Upload(ctx context.Context, wav []byte, recordedAt time.Time) (Result, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("audio", "clip.wav")
	if err != nil {
		return Result{}, err
	}
	if _, err := fw.Write(wav); err != nil {
		return Result{}, err
	}
	if err := mw.WriteField("recordedAt", recordedAt.UTC().Format(time.RFC3339)); err != nil {
		return Result{}, err
	}
	if err := mw.Close(); err != nil {
		return Result{}, err
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/api/listener/clips", mw.FormDataContentType(), &body)
	if err != nil {
		return Result{}, fmt.Errorf("uploading clip: %w", err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return Result{}, ErrUnauthorized
	default:
		return Result{}, apiError("uploading clip", status, raw)
	}
	var out struct {
		Mode        string `json:"mode"`
		Recognition struct {
			Outcome      string  `json:"outcome"`
			MatchedTitle *string `json:"matchedTitle"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("reading the server's answer: %w", err)
	}
	r := Result{Mode: out.Mode, Outcome: out.Recognition.Outcome}
	if out.Recognition.MatchedTitle != nil {
		r.Title = *out.Recognition.MatchedTitle
	}
	return r, nil
}

// Heartbeat tells the server the listener is alive and how loud the room is.
func (c *Client) Heartbeat(ctx context.Context, levelDBFS float64, mode string) error {
	payload, err := json.Marshal(map[string]any{"level": levelDBFS, "mode": mode})
	if err != nil {
		return err
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/api/listener/heartbeat", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("sending heartbeat: %w", err)
	}
	switch status {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	}
	return apiError("sending heartbeat", status, raw)
}

// SignOff tells the server this listener is stopping, so it shows as offline at once instead of
// after its heartbeats time out.
func (c *Client) SignOff(ctx context.Context) error {
	status, raw, err := c.do(ctx, http.MethodPost, "/api/listener/sign-off", "", nil)
	if err != nil {
		return fmt.Errorf("signing off: %w", err)
	}
	if status != http.StatusNoContent {
		return apiError("signing off", status, raw)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error would repeat the address; the cause is enough.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, err
}

// apiError describes a failed call, with the server's message when it sent one.
func apiError(what string, status int, raw []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("%s: HTTP %d: %s", what, status, e.Error.Message)
	}
	return fmt.Errorf("%s: HTTP %d", what, status)
}
