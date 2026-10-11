// Package server talks to the Now Playing server with the client generated from api/openapi.yaml.
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/godwinrob/now-playing/apps/listener/internal/apiclient/gen"
)

// ErrUnauthorized means the server rejected the device token.
var ErrUnauthorized = errors.New("the server rejected the device token (check NPL_DEVICE_TOKEN against NP_DEVICE_TOKEN)")

// Result is what the server made of a clip.
type Result struct {
	Mode    string // "searching" or "tracking": what to do next
	Outcome string // matched, unmatched, no_result or error
	Title   string // the matched record, when there is one
}

type Client struct {
	api *gen.ClientWithResponses
}

func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	api, err := gen.NewClientWithResponses(baseURL,
		gen.WithHTTPClient(httpClient),
		gen.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}))
	if err != nil {
		return nil, fmt.Errorf("creating API client: %w", err)
	}
	return &Client{api: api}, nil
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
	resp, err := c.api.ListenWithBodyWithResponse(ctx, mw.FormDataContentType(), &body)
	if err != nil {
		return Result{}, fmt.Errorf("uploading clip: %w", err)
	}
	switch {
	case resp.StatusCode() == http.StatusUnauthorized:
		return Result{}, ErrUnauthorized
	case resp.JSON200 == nil:
		return Result{}, apiError("uploading clip", resp.StatusCode(), resp.JSON400)
	}
	r := Result{Mode: string(resp.JSON200.Mode), Outcome: string(resp.JSON200.Recognition.Outcome)}
	if t := resp.JSON200.Recognition.MatchedTitle; t != nil {
		r.Title = *t
	}
	return r, nil
}

// Heartbeat tells the server the listener is alive and how loud the room is.
func (c *Client) Heartbeat(ctx context.Context, levelDBFS float64, mode string) error {
	resp, err := c.api.HeartbeatWithResponse(ctx, gen.HeartbeatJSONRequestBody{Level: float32(levelDBFS), Mode: gen.HeartbeatRequestMode(mode)})
	if err != nil {
		return fmt.Errorf("sending heartbeat: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	}
	return apiError("sending heartbeat", resp.StatusCode(), nil)
}

func apiError(what string, status int, e *gen.Error) error {
	if e != nil {
		return fmt.Errorf("%s: HTTP %d: %s", what, status, e.Error.Message)
	}
	return fmt.Errorf("%s: HTTP %d", what, status)
}
