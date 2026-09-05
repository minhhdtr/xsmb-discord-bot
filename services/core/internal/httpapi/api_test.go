package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/httpapi"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fixedSource answers for any day, so a test can decide what the source has
// without scripting call order.
type fixedSource struct {
	outcome func(day time.Time) domain.Outcome
}

func (f fixedSource) Fetch(_ context.Context, day time.Time) domain.Outcome {
	return f.outcome(day)
}

func (fixedSource) Name() string { return "test" }

func drawFor(t *testing.T, day time.Time) domain.Draw {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			at++
			numbers = append(numbers, fmt.Sprintf("%0*d", spec.Digits, at))
		}
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Draw{Date: domain.DayOf(day), Prizes: prizes,
		Source: "test", FetchedAt: day}
}

// newServer wires a server over an in-memory store with the clock frozen at
// 20:00, comfortably past the completion mark.
func newServer(t *testing.T, out func(time.Time) domain.Outcome) (*httpapi.Server, storage.Store) {
	t.Helper()
	store := storage.NewMemory()
	clock := func() time.Time {
		return time.Date(2026, 8, 21, 20, 0, 0, 0, domain.Location())
	}
	svc := service.New(store, fixedSource{outcome: out}, clock, quiet())
	return httpapi.New(svc, store, nil, quiet()), store
}

func get(t *testing.T, s *httpapi.Server, path string) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.Bytes()
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("cannot decode %s: %v", body, err)
	}
	return out
}

func found(t *testing.T) func(time.Time) domain.Outcome {
	return func(day time.Time) domain.Outcome { return domain.Found(drawFor(t, day)) }
}

func TestDrawCarriesRawNumbersAndARenderedTable(t *testing.T) {
	s, _ := newServer(t, found(t))

	status, body := get(t, s, "/v1/draws/2026-08-20")
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	draw := decode[map[string]any](t, body)

	if draw["date"] != "2026-08-20" {
		t.Errorf("date = %v", draw["date"])
	}
	if numbers, _ := draw["numbers"].([]any); len(numbers) != domain.TotalNumbers {
		t.Errorf("numbers has %d entries, want %d", len(numbers), domain.TotalNumbers)
	}
	// Both halves of the decision: the client can take the drawn table, or
	// draw its own from the numbers.
	if table, _ := draw["table"].(string); table == "" {
		t.Error("table is empty")
	}
	if head, _ := draw["head_tail"].(string); head == "" {
		t.Error("head_tail is empty")
	}
}

// The wire uses ISO dates. The Vietnamese forms are what a person types, and
// parsing those is the client's job.
func TestDrawRejectsAVietnameseDate(t *testing.T) {
	s, _ := newServer(t, found(t))

	status, body := get(t, s, "/v1/draws/20-08-2026")
	if status != http.StatusBadRequest {
		t.Fatalf("status %d: %s", status, body)
	}
	if code := decode[map[string]any](t, body)["code"]; code != "bad_request" {
		t.Errorf("code = %v", code)
	}
}

// not_yet and no_draw are both "no draw here", but one is worth asking again
// for and the other never will be. Flattening them would leave a client unable
// to tell whether to keep polling.
func TestNotYetAndNoDrawAreDifferentCodes(t *testing.T) {
	early := time.Date(2026, 8, 21, 18, 30, 0, 0, domain.Location())

	notYet := httpapi.New(service.New(storage.NewMemory(),
		fixedSource{outcome: func(time.Time) domain.Outcome {
			return domain.Failed(domain.ErrIncomplete)
		}}, func() time.Time { return early }, quiet()), storage.NewMemory(), nil, quiet())

	status, body := get(t, notYet, "/v1/draws/2026-08-21")
	if status != http.StatusNotFound {
		t.Fatalf("status %d: %s", status, body)
	}
	if code := decode[map[string]any](t, body)["code"]; code != "not_yet" {
		t.Errorf("code = %v, want not_yet", code)
	}

	settled, _ := newServer(t, func(time.Time) domain.Outcome {
		return domain.Failed(provider.ErrNoData)
	})
	status, body = get(t, settled, "/v1/draws/2011-03-07")
	if status != http.StatusNotFound {
		t.Fatalf("status %d: %s", status, body)
	}
	if code := decode[map[string]any](t, body)["code"]; code != "no_draw" {
		t.Errorf("code = %v, want no_draw", code)
	}
}

func TestFutureDateIsOutOfRange(t *testing.T) {
	s, _ := newServer(t, found(t))

	status, body := get(t, s, "/v1/draws/2030-01-01")
	if status != http.StatusBadRequest {
		t.Fatalf("status %d: %s", status, body)
	}
	if code := decode[map[string]any](t, body)["code"]; code != "out_of_range" {
		t.Errorf("code = %v", code)
	}
}

func TestFrequencyGroupsOnlyWhenAsked(t *testing.T) {
	s, _ := newServer(t, found(t))
	if _, err := decodeErr(s, "/v1/draws/2026-08-20"); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, s, "/v1/stats/frequency?days=30")
	if grouped := decode[map[string]any](t, body)["grouped"]; grouped != false {
		t.Errorf("grouped = %v without a group parameter", grouped)
	}

	_, body = get(t, s, "/v1/stats/frequency?days=30&group=cham")
	got := decode[map[string]any](t, body)
	if got["grouped"] != true {
		t.Errorf("grouped = %v with group=cham", got["grouped"])
	}
	// Chạm is the one grouping where a number falls in two buckets, and a
	// client showing the total needs to know that.
	if got["overlaps"] != true {
		t.Errorf("overlaps = %v for cham", got["overlaps"])
	}
	if buckets, _ := got["buckets"].([]any); len(buckets) != 10 {
		t.Errorf("got %d buckets, want 10", len(buckets))
	}
	if table, _ := got["table"].(string); table == "" {
		t.Error("grouped frequency has no table")
	}
}

func TestBadGroupIsRejected(t *testing.T) {
	s, _ := newServer(t, found(t))
	status, body := get(t, s, "/v1/stats/frequency?group=xyz")
	if status != http.StatusBadRequest {
		t.Fatalf("status %d: %s", status, body)
	}
}

// A limit outside the range is the caller's bug. Clamping it silently would
// hide that.
func TestOutOfRangeLimitIsRejected(t *testing.T) {
	s, _ := newServer(t, found(t))
	status, _ := get(t, s, "/v1/stats/gan?limit=500")
	if status != http.StatusBadRequest {
		t.Errorf("status %d, want 400", status)
	}
}

func TestSpinNeverTouchesTheArchive(t *testing.T) {
	s, store := newServer(t, found(t))

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/spins", nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	spin := decode[map[string]any](t, rec.Body.Bytes())

	numbers, _ := spin["numbers"].([]any)
	order, _ := spin["order"].([]any)
	if len(numbers) != domain.TotalNumbers || len(order) != domain.TotalNumbers {
		t.Fatalf("numbers=%d order=%d", len(numbers), len(order))
	}
	// The special is stored first and revealed last.
	if last := order[len(order)-1]; last != float64(0) {
		t.Errorf("last reveal is index %v, want 0", last)
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Draws != 0 {
		t.Errorf("a spin wrote %d draws to the archive", stats.Draws)
	}
}

func TestSpinRejectsGet(t *testing.T) {
	s, _ := newServer(t, found(t))
	if status, _ := get(t, s, "/v1/spins"); status != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", status)
	}
}

func TestHealthAndArchive(t *testing.T) {
	s, _ := newServer(t, found(t))

	if status, body := get(t, s, "/healthz"); status != http.StatusOK ||
		decode[map[string]any](t, body)["status"] != "ok" {
		t.Errorf("healthz: %d %s", status, body)
	}
	status, body := get(t, s, "/v1/archive")
	if status != http.StatusOK {
		t.Fatalf("archive: %d %s", status, body)
	}
	// An empty archive reports null dates rather than the zero time, which
	// would decode as year 1.
	got := decode[map[string]any](t, body)
	if got["earliest"] != nil {
		t.Errorf("earliest = %v on an empty archive", got["earliest"])
	}
}

// A nil slice marshals as null, and a client iterating the field should not
// have to guard against that when the honest answer is "none".
func TestEmptyListsAreArraysNotNull(t *testing.T) {
	s, _ := newServer(t, found(t))
	if _, err := decodeErr(s, "/v1/draws/2026-08-20"); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, s, "/v1/stats/day?date=2026-08-20")
	got := decode[map[string]any](t, body)
	for _, field := range []string{"kep", "nhay", "mute_heads", "mute_tails", "top_heads", "top_tails"} {
		if _, ok := got[field].([]any); !ok {
			t.Errorf("%s = %v, want an array", field, got[field])
		}
	}

	_, body = get(t, s, "/v1/stats/gan")
	if _, ok := decode[any](t, body).([]any); !ok {
		t.Error("gan did not return an array")
	}
}

// decodeErr fetches a path and reports a non-2xx as an error, for setup steps
// where the response body is not the point.
func decodeErr(s *httpapi.Server, path string) ([]byte, error) {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code/100 != 2 {
		return nil, fmt.Errorf("%s: status %d: %s", path, rec.Code, rec.Body)
	}
	return rec.Body.Bytes(), nil
}

// --- subscriptions and announcement claims ---

func send(t *testing.T, s *httpapi.Server, method, path, body string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	return rec.Code, rec.Body.Bytes()
}

// Both writes are idempotent, and the flag is what tells a person "đã bật"
// from "vốn đã bật".
func TestSubscribeAndUnsubscribeReportWhetherAnythingChanged(t *testing.T) {
	s, _ := newServer(t, found(t))

	status, body := send(t, s, http.MethodPut, "/v1/subscriptions/chan-1", `{"guild_id":"g1"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	if created := decode[map[string]any](t, body)["created"]; created != true {
		t.Errorf("created = %v on a new subscription", created)
	}

	_, body = send(t, s, http.MethodPut, "/v1/subscriptions/chan-1", `{"guild_id":"g1"}`)
	if created := decode[map[string]any](t, body)["created"]; created != false {
		t.Errorf("created = %v the second time", created)
	}

	_, body = get(t, s, "/v1/subscriptions")
	if subs := decode[[]map[string]any](t, body); len(subs) != 1 || subs[0]["channel_id"] != "chan-1" {
		t.Fatalf("subscriptions = %v", subs)
	}

	_, body = send(t, s, http.MethodDelete, "/v1/subscriptions/chan-1", "")
	if removed := decode[map[string]any](t, body)["removed"]; removed != true {
		t.Errorf("removed = %v", removed)
	}
	_, body = send(t, s, http.MethodDelete, "/v1/subscriptions/chan-1", "")
	if removed := decode[map[string]any](t, body)["removed"]; removed != false {
		t.Errorf("removed = %v on the second delete", removed)
	}
}

// The guild id is a convenience for whoever reads the table, not a
// requirement. A subscription without a body still works.
func TestSubscribeWithoutABody(t *testing.T) {
	s, _ := newServer(t, found(t))
	if status, body := send(t, s, http.MethodPut, "/v1/subscriptions/chan-2", ""); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
}

func TestSubscribeRejectsAMalformedBody(t *testing.T) {
	s, _ := newServer(t, found(t))
	status, _ := send(t, s, http.MethodPut, "/v1/subscriptions/chan-3", "khong-phai-json")
	if status != http.StatusBadRequest {
		t.Errorf("status %d, want 400", status)
	}
}

// This is what stops a restart at 18:50 from posting the same result twice.
func TestAnnouncementIsClaimedExactlyOnce(t *testing.T) {
	s, _ := newServer(t, found(t))

	status, _ := send(t, s, http.MethodPost, "/v1/announcements/2026-08-20/chan-1", "")
	if status != http.StatusCreated {
		t.Fatalf("first claim: status %d, want 201", status)
	}

	status, body := send(t, s, http.MethodPost, "/v1/announcements/2026-08-20/chan-1", "")
	if status != http.StatusConflict {
		t.Fatalf("second claim: status %d, want 409", status)
	}
	if code := decode[map[string]any](t, body)["code"]; code != "already_claimed" {
		t.Errorf("code = %v", code)
	}

	// A different channel, and a different day, are separate claims.
	if status, _ := send(t, s, http.MethodPost, "/v1/announcements/2026-08-20/chan-2", ""); status != http.StatusCreated {
		t.Errorf("another channel was blocked: %d", status)
	}
	if status, _ := send(t, s, http.MethodPost, "/v1/announcements/2026-08-19/chan-1", ""); status != http.StatusCreated {
		t.Errorf("another day was blocked: %d", status)
	}
}

// A failed send gives the claim back, so the next run tries again rather than
// the day being lost.
func TestReleasingAClaimLetsItBeTakenAgain(t *testing.T) {
	s, _ := newServer(t, found(t))

	send(t, s, http.MethodPost, "/v1/announcements/2026-08-20/chan-1", "")
	if status, _ := send(t, s, http.MethodDelete, "/v1/announcements/2026-08-20/chan-1", ""); status != http.StatusNoContent {
		t.Fatalf("release: status %d, want 204", status)
	}
	if status, _ := send(t, s, http.MethodPost, "/v1/announcements/2026-08-20/chan-1", ""); status != http.StatusCreated {
		t.Errorf("claim after release: status %d, want 201", status)
	}
}
