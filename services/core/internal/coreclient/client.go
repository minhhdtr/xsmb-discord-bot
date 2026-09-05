package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Client talks to core over the contract in contracts/openapi.yaml.
//
// It returns domain types rather than wire types, and service sentinel errors
// rather than status codes, so a caller reads the same as it did when the
// service was in the same process. That is the point: the move from a function
// call to a network call should not ripple out into the command handlers.
type Client struct {
	base string
	http *http.Client
}

// New points a client at core. baseURL has no trailing slash.
func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		// Long enough to cover a crawl of a day the archive is missing, which
		// is the slowest thing behind any of these routes.
		timeout = 20 * time.Second
	}
	return &Client{
		base: strings.TrimSuffix(baseURL, "/"),
		http: &http.Client{Timeout: timeout},
	}
}

// wireError is the body every failure carries.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Sentinels for the codes that have no service equivalent.
var (
	// ErrAlreadyClaimed means another run already took the right to announce.
	ErrAlreadyClaimed = errors.New("announcement already claimed")
	// ErrBadRequest means core rejected the input.
	ErrBadRequest = errors.New("core rejected the request")
	// ErrNotConfigured means the feature is switched off in this deployment.
	ErrNotConfigured = errors.New("feature not configured")
)

// do performs a request and decodes into out, which may be nil for endpoints
// that answer with a status alone.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("core unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return c.decodeError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("core sent a shape this client cannot read: %w", err)
	}
	return nil
}

// decodeError turns a failure body back into the sentinel a caller expects.
//
// The mapping runs off `code`, never off the status: two 404s here mean
// different things, and only the code tells them apart.
func (c *Client) decodeError(resp *http.Response) error {
	var wire wireError
	_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&wire)

	switch wire.Code {
	case "not_yet":
		return fmt.Errorf("%s: %w", wire.Message, domain.ErrNotYet)
	case "no_draw":
		return fmt.Errorf("%s: %w", wire.Message, domain.ErrNoResult)
	case "out_of_range":
		return fmt.Errorf("%s: %w", wire.Message, domain.ErrOutOfRange)
	case "already_claimed":
		return ErrAlreadyClaimed
	case "not_configured":
		return fmt.Errorf("%s: %w", wire.Message, ErrNotConfigured)
	case "bad_request":
		return fmt.Errorf("%s: %w", wire.Message, ErrBadRequest)
	}
	if wire.Message != "" {
		return fmt.Errorf("core: %s (%s)", wire.Message, resp.Status)
	}
	return fmt.Errorf("core: %s", resp.Status)
}

func query(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

func itoa(n int) string { return strconv.Itoa(n) }
