package provider_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
)

var classes = [domain.PrizeGroups]string{
	"special-prize", "prize1", "prize2", "prize3",
	"prize4", "prize5", "prize6", "prize7",
}

type pageOpts struct {
	dropTier     int // 1-based tier to render one number short; 0 = none
	repeatTable  bool
	stripZeros   bool
	brSeparated  bool
	extraMarkup  bool
	emptyResults bool
}

// buildPage renders a result page: one element per number, carrying the
// tier's CSS class.
func buildPage(o pageOpts) string {
	if o.emptyResults {
		return `<html><body><div class="content"><p>Chưa có kết quả</p></div></body></html>`
	}
	var b strings.Builder
	b.WriteString(`<html><head><title>XSMB</title></head><body>`)
	if o.extraMarkup {
		b.WriteString(`<script>var d="<td class=\"prize1\">99999</td>";</script>`)
		b.WriteString(`<div class="ads">Quảng cáo 12345</div>`)
	}
	table := func() string {
		var t strings.Builder
		t.WriteString(`<table class="result"><tbody>`)
		for i, spec := range domain.PrizeLayout {
			t.WriteString(`<tr>`)
			if o.brSeparated {
				t.WriteString(`<td class="` + classes[i] + `">`)
				for n := 0; n < spec.Count; n++ {
					if o.dropTier == i+1 && n == 0 {
						continue
					}
					if n > 0 {
						t.WriteString("<br>")
					}
					t.WriteString(number(n+1, spec.Digits, o.stripZeros))
				}
				t.WriteString(`</td>`)
			} else {
				for n := 0; n < spec.Count; n++ {
					if o.dropTier == i+1 && n == 0 {
						continue
					}
					t.WriteString(`<td class="` + classes[i] + `"><span>` +
						number(n+1, spec.Digits, o.stripZeros) + `</span></td>`)
				}
			}
			t.WriteString(`</tr>`)
		}
		t.WriteString(`</tbody></table>`)
		return t.String()
	}()
	b.WriteString(table)
	if o.repeatTable {
		b.WriteString(`<div class="mobile">` + table + `</div>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func number(n, digits int, strip bool) string {
	s := fmt.Sprintf("%0*d", digits, n)
	if strip {
		s = strings.TrimLeft(s, "0")
		if s == "" {
			s = "0"
		}
	}
	return s
}

func day() time.Time { return domain.NewDate(2026, 8, 14) }

func serve(t *testing.T, handler http.HandlerFunc) *provider.Xoso {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return provider.NewXoso(
		provider.WithBaseURL(server.URL),
		provider.WithBackoff(time.Millisecond),
		provider.WithLogger(quietLog()),
	)
}

func TestParseCompletePage(t *testing.T) {
	prizes, err := provider.Parse(buildPage(pageOpts{}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(prizes.Numbers()); got != domain.TotalNumbers {
		t.Fatalf("got %d numbers, want 27", got)
	}
	if got := prizes.Special(); got != "00001" {
		t.Fatalf("special = %q", got)
	}
	if got := prizes.Group(7); len(got) != 4 || got[0] != "01" || got[3] != "04" {
		t.Fatalf("prize7 = %v", got)
	}
}

func TestParseHandlesBrSeparatedCells(t *testing.T) {
	prizes, err := provider.Parse(buildPage(pageOpts{brSeparated: true}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := prizes.Group(3); len(got) != 6 {
		t.Fatalf("prize3 = %v", got)
	}
}

func TestParsePadsStrippedLeadingZeros(t *testing.T) {
	prizes, err := provider.Parse(buildPage(pageOpts{stripZeros: true}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := prizes.Special(); got != "00001" {
		t.Fatalf("special = %q, want zero-padded", got)
	}
}

func TestParseCollapsesRepeatedTable(t *testing.T) {
	prizes, err := provider.Parse(buildPage(pageOpts{repeatTable: true}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(prizes.Numbers()); got != domain.TotalNumbers {
		t.Fatalf("got %d numbers, want 27", got)
	}
}

func TestParseIgnoresScriptsAndUnrelatedNumbers(t *testing.T) {
	if _, err := provider.Parse(buildPage(pageOpts{extraMarkup: true})); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

// A mid-draw page is missing tiers and must never validate - that's what
// keeps the announcer polling.
func TestParseRejectsPartialPage(t *testing.T) {
	for tier := 1; tier <= domain.PrizeGroups; tier++ {
		_, err := provider.Parse(buildPage(pageOpts{dropTier: tier}))
		if !errors.Is(err, domain.ErrIncomplete) {
			t.Fatalf("tier %d: err = %v, want ErrIncomplete", tier, err)
		}
	}
}

func TestParseReportsNoDataSeparately(t *testing.T) {
	_, err := provider.Parse(buildPage(pageOpts{emptyResults: true}))
	if !errors.Is(err, provider.ErrNoData) {
		t.Fatalf("err = %v, want ErrNoData", err)
	}
}

func TestFetchFound(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xsmb-14-08-2026.html" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" || !strings.Contains(r.Header.Get("Accept-Language"), "vi") {
			t.Errorf("browser headers missing")
		}
		fmt.Fprint(w, buildPage(pageOpts{}))
	})
	out := x.Fetch(context.Background(), day())
	if out.Status != domain.StatusFound {
		t.Fatalf("status = %v, err = %v", out.Status, out.Err)
	}
	if out.Draw.Source != "xoso.com.vn" || !out.Draw.Date.Equal(day()) {
		t.Fatalf("draw = %+v", out.Draw)
	}
}

func TestFetch404IsAbsentNotFailure(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if out := x.Fetch(context.Background(), day()); out.Status != domain.StatusAbsent {
		t.Fatalf("status = %v, want Absent", out.Status)
	}
}

func TestFetchRetriesThenSucceeds(t *testing.T) {
	var hits int32
	x := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, buildPage(pageOpts{}))
	})
	if out := x.Fetch(context.Background(), day()); out.Status != domain.StatusFound {
		t.Fatalf("status = %v, err = %v", out.Status, out.Err)
	}
	if hits != 3 {
		t.Fatalf("hits = %d, want 3", hits)
	}
}

// A dead upstream is a failure, never an absence.
func TestFetchServerErrorIsFailureNotAbsence(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	out := x.Fetch(context.Background(), day())
	if out.Status != domain.StatusFailed {
		t.Fatalf("status = %v, want Failed", out.Status)
	}
}

func TestFetchDetectsBotWallByStatus(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	out := x.Fetch(context.Background(), day())
	if !errors.Is(out.Err, provider.ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", out.Err)
	}
}

func TestFetchDetectsBotWallByBody(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Just a moment...</title></head><body>cf_chl_opt</body></html>`)
	})
	out := x.Fetch(context.Background(), day())
	if !errors.Is(out.Err, provider.ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", out.Err)
	}
}

func TestFetchPartialPageIsFailureWithIncomplete(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, buildPage(pageOpts{dropTier: 8}))
	})
	out := x.Fetch(context.Background(), day())
	if out.Status != domain.StatusFailed || !errors.Is(out.Err, domain.ErrIncomplete) {
		t.Fatalf("status = %v, err = %v", out.Status, out.Err)
	}
}

func TestFetchHonoursContextCancellation(t *testing.T) {
	x := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := x.Fetch(ctx, day())
	if out.Status != domain.StatusFailed {
		t.Fatalf("status = %v, want Failed", out.Status)
	}
}

func TestURLFormat(t *testing.T) {
	x := provider.NewXoso()
	if got := x.URL(domain.NewDate(2005, 10, 3)); got != "https://xoso.com.vn/xsmb-03-10-2005.html" {
		t.Fatalf("URL = %q", got)
	}
}
