package provider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/htmlscan"
)

// DefaultBaseURL is the archive host. Every draw has its own page.
const DefaultBaseURL = "https://xoso.com.vn"

// cssClasses maps each tier of domain.PrizeLayout to its class on the page.
var cssClasses = [domain.PrizeGroups]string{
	"special-prize", "prize1", "prize2", "prize3",
	"prize4", "prize5", "prize6", "prize7",
}

// botWallMarkers appear in interstitial pages served by bot protection
// instead of the real document.
var botWallMarkers = []string{
	"just a moment", "cf-browser-verification", "cf_chl_opt",
	"checking your browser", "enable javascript and cookies",
	"attention required! | cloudflare", "__cf_chl",
}

// Xoso reads results from xoso.com.vn.
type Xoso struct {
	client   *http.Client
	baseURL  string
	attempts int
	backoff  time.Duration
	log      *slog.Logger
}

// Option customises a Xoso.
type Option func(*Xoso)

// WithBaseURL points the crawler elsewhere - tests, or a mirror.
func WithBaseURL(url string) Option {
	return func(x *Xoso) { x.baseURL = strings.TrimSuffix(url, "/") }
}

// WithHTTPClient replaces the transport. Swap in a uTLS client here if plain
// net/http ever gets fingerprinted.
func WithHTTPClient(c *http.Client) Option { return func(x *Xoso) { x.client = c } }

// WithAttempts sets how many times a transient failure is retried.
func WithAttempts(n int) Option { return func(x *Xoso) { x.attempts = n } }

// WithBackoff sets the pause between attempts; it doubles each time.
func WithBackoff(d time.Duration) Option { return func(x *Xoso) { x.backoff = d } }

// WithLogger attaches a logger.
func WithLogger(l *slog.Logger) Option { return func(x *Xoso) { x.log = l } }

// NewXoso builds a crawler with browser-like defaults.
func NewXoso(opts ...Option) *Xoso {
	x := &Xoso{
		client:   &http.Client{Timeout: 20 * time.Second},
		baseURL:  DefaultBaseURL,
		attempts: 3,
		backoff:  700 * time.Millisecond,
		log:      slog.Default(),
	}
	for _, opt := range opts {
		opt(x)
	}
	return x
}

// Name identifies the source in stored rows and in the Discord footer.
func (x *Xoso) Name() string { return "xoso.com.vn" }

// URL is the page for one day.
func (x *Xoso) URL(day time.Time) string {
	return fmt.Sprintf("%s/xsmb-%s.html", x.baseURL, domain.DayOf(day).Format("02-01-2006"))
}

// Fetch downloads and parses one day.
func (x *Xoso) Fetch(ctx context.Context, day time.Time) domain.Outcome {
	body, outcome := x.download(ctx, day)
	if outcome != nil {
		return *outcome
	}
	prizes, err := Parse(body)
	if err != nil {
		return domain.Failed(fmt.Errorf("%s %s: %w", x.Name(), domain.FormatVN(day), err))
	}
	return domain.Found(domain.Draw{
		Date:      domain.DayOf(day),
		Prizes:    prizes,
		Source:    x.Name(),
		FetchedAt: time.Now().In(domain.Location()),
	})
}

// download returns the page body, or a non-nil Outcome that ends the fetch.
func (x *Xoso) download(ctx context.Context, day time.Time) (string, *domain.Outcome) {
	url := x.URL(day)
	var last error

	for attempt := 1; attempt <= x.attempts; attempt++ {
		if attempt > 1 {
			pause := x.backoff * time.Duration(1<<(attempt-2))
			select {
			case <-ctx.Done():
				out := domain.Failed(ctx.Err())
				return "", &out
			case <-time.After(pause):
			}
		}

		body, status, err := x.get(ctx, url)
		switch {
		case err != nil:
			last = err
			x.log.Warn("fetch failed", "source", x.Name(), "date", domain.FormatISO(day),
				"attempt", attempt, "error", err)
		case status == http.StatusNotFound || status == http.StatusGone:
			// The archive is explicit: this day has no page. Safe to cache.
			out := domain.Absent()
			return "", &out
		case status == http.StatusForbidden || status == http.StatusTooManyRequests:
			out := domain.Failed(fmt.Errorf("%s: HTTP %d: %w", x.Name(), status, ErrBlocked))
			return "", &out
		case status >= 500:
			last = fmt.Errorf("%s: HTTP %d", x.Name(), status)
			x.log.Warn("upstream error", "source", x.Name(), "date", domain.FormatISO(day),
				"attempt", attempt, "status", status)
		case status != http.StatusOK:
			out := domain.Failed(fmt.Errorf("%s: HTTP %d", x.Name(), status))
			return "", &out
		case looksLikeBotWall(body):
			out := domain.Failed(fmt.Errorf("%s: %w", x.Name(), ErrBlocked))
			return "", &out
		default:
			return body, nil
		}
	}
	out := domain.Failed(fmt.Errorf("%s: giving up on %s after %d attempts: %w",
		x.Name(), url, x.attempts, last))
	return "", &out
}

func (x *Xoso) get(ctx context.Context, url string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	// Look like a browser navigation. Accept-Encoding is left unset so net/http
	// negotiates gzip itself.
	for key, value := range map[string]string{
		"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"Accept-Language":           "vi-VN,vi;q=0.9,en-US;q=0.8,en;q=0.7",
		"Cache-Control":             "no-cache",
		"Pragma":                    "no-cache",
		"Referer":                   x.baseURL + "/",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "same-origin",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	} {
		req.Header.Set(key, value)
	}

	resp, err := x.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	// 4 MB is far above any real page and bounds a hostile response.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(body), resp.StatusCode, nil
}

func looksLikeBotWall(body string) bool {
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	head = strings.ToLower(head)
	for _, marker := range botWallMarkers {
		if strings.Contains(head, marker) {
			return true
		}
	}
	return false
}

// Parse pulls 27 numbers out of a result page. No network, so it can be
// tested against saved HTML.
func Parse(html string) (domain.Prizes, error) {
	var groups [domain.PrizeGroups][]string
	empty := 0

	for i, spec := range domain.PrizeLayout {
		found := numbersIn(htmlscan.TextByClass(html, cssClasses[i]), spec)
		if len(found) == 0 {
			empty++
		}
		groups[i] = dropRepeatedTable(found, spec.Count)
	}

	if empty == domain.PrizeGroups {
		return domain.Prizes{}, ErrNoData
	}
	prizes, err := domain.NewPrizesByGroup(groups)
	if err != nil {
		return domain.Prizes{}, err
	}
	return prizes, nil
}

// numbersIn pulls numbers of the tier's width out of matched elements. One
// element may hold one number or several. Runs wider than the tier are
// dropped, so a page that concatenates numbers fails instead of returning
// plausible garbage.
func numbersIn(texts []string, spec domain.PrizeSpec) []string {
	out := make([]string, 0, spec.Count)
	for _, text := range texts {
		for _, run := range digitRuns(text) {
			if len(run) > spec.Digits {
				continue
			}
			out = append(out, strings.Repeat("0", spec.Digits-len(run))+run)
		}
	}
	return out
}

// dropRepeatedTable collapses a table the page renders twice - a desktop and
// a mobile copy. Only exact repetition; anything else fails validation.
func dropRepeatedTable(found []string, count int) []string {
	if count <= 0 || len(found) <= count || len(found)%count != 0 {
		return found
	}
	for i := count; i < len(found); i++ {
		if found[i] != found[i%count] {
			return found
		}
	}
	return found[:count]
}

func digitRuns(s string) []string {
	var runs []string
	start := -1
	for i := 0; i < len(s); i++ {
		isDigit := s[i] >= '0' && s[i] <= '9'
		if isDigit && start < 0 {
			start = i
		}
		if !isDigit && start >= 0 {
			runs = append(runs, s[start:i])
			start = -1
		}
	}
	if start >= 0 {
		runs = append(runs, s[start:])
	}
	return runs
}
