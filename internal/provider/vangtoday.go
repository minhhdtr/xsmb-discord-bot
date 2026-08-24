package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// DefaultGoldURL is the aggregator endpoint. No key needed.
const DefaultGoldURL = "https://www.vang.today/api/prices"

// GoldProvider is one source of gold prices.
type GoldProvider interface {
	Name() string
	Board(ctx context.Context) (domain.GoldBoard, error)
	// History returns up to days of daily quotes for one gold type.
	History(ctx context.Context, code string, days int) (domain.GoldSeries, error)
}

// Display order. The upstream `prices` field is a JSON object, so decoding
// gives a map with no order. Codes missing from this list are appended
// alphabetically rather than dropped.
var displayOrder = []string{
	"XAUUSD",      // world spot first: it is the context for everything below
	"SJL1L10",     // vàng miếng SJC
	"SJ9999",      // nhẫn SJC
	"VNGSJC",      // VN Gold
	"BTSJC",       // Bảo Tín, miếng SJC
	"BT9999NTT",   // Bảo Tín 9999
	"DOHNL",       // DOJI Hà Nội
	"DOHCML",      // DOJI HCM
	"DOJINHTV",    // DOJI nữ trang
	"PQHNVM",      // PNJ Hà Nội
	"PQHN24NTT",   // PNJ 24K
	"VIETTINMSJC", // VietinBank
}

// displayNames translates the upstream English labels. Unlisted codes keep
// the name the API gave, so this table can't hide a new row.
var displayNames = map[string]string{
	"XAUUSD":      "Vàng thế giới",
	"SJL1L10":     "Vàng miếng SJC",
	"SJ9999":      "Nhẫn SJC 9999",
	"VNGSJC":      "VN Gold SJC",
	"BTSJC":       "Bảo Tín · miếng SJC",
	"BT9999NTT":   "Bảo Tín 9999",
	"DOHNL":       "DOJI Hà Nội",
	"DOHCML":      "DOJI HCM",
	"DOJINHTV":    "DOJI nữ trang",
	"PQHNVM":      "PNJ Hà Nội",
	"PQHN24NTT":   "PNJ 24K",
	"VIETTINMSJC": "VietinBank SJC",
}

// GoldCodes lists the type codes the source publishes, in display order.
func GoldCodes() []string {
	out := make([]string, len(displayOrder))
	copy(out, displayOrder)
	return out
}

// VangToday reads the aggregated board from vang.today.
type VangToday struct {
	client *http.Client
	url    string
	log    *slog.Logger
}

// GoldOption customises a VangToday.
type GoldOption func(*VangToday)

// WithGoldURL points the client somewhere else; tests use it for httptest.
func WithGoldURL(url string) GoldOption { return func(v *VangToday) { v.url = url } }

// WithGoldHTTPClient replaces the transport.
func WithGoldHTTPClient(c *http.Client) GoldOption { return func(v *VangToday) { v.client = c } }

// WithGoldLogger attaches a logger.
func WithGoldLogger(l *slog.Logger) GoldOption { return func(v *VangToday) { v.log = l } }

// NewVangToday builds a gold price client.
func NewVangToday(opts ...GoldOption) *VangToday {
	v := &VangToday{
		client: &http.Client{Timeout: 15 * time.Second},
		url:    DefaultGoldURL,
		log:    slog.Default(),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Name identifies the source in the embed footer.
func (v *VangToday) Name() string { return "vang.today" }

// goldResponse mirrors the upstream payload.
type goldResponse struct {
	Success   bool                 `json:"success"`
	Timestamp int64                `json:"timestamp"`
	Date      string               `json:"date"`
	Time      string               `json:"time"`
	Count     int                  `json:"count"`
	Prices    map[string]goldEntry `json:"prices"`
}

type goldEntry struct {
	Name       string  `json:"name"`
	Buy        float64 `json:"buy"`
	Sell       float64 `json:"sell"`
	ChangeBuy  float64 `json:"change_buy"`
	ChangeSell float64 `json:"change_sell"`
	Currency   string  `json:"currency"`
}

// Board fetches and parses the current board.
func (v *VangToday) Board(ctx context.Context) (domain.GoldBoard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return domain.GoldBoard{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xsmb-discord-bot/1.0 (+discord)")

	resp, err := v.client.Do(req)
	if err != nil {
		return domain.GoldBoard{}, fmt.Errorf("%s: %w", v.Name(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.GoldBoard{}, fmt.Errorf("%s: HTTP %d", v.Name(), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return domain.GoldBoard{}, fmt.Errorf("%s: %w", v.Name(), err)
	}
	return ParseGold(body, v.Name(), time.Now().In(domain.Location()), v.log)
}

// ParseGold turns the payload into a board. No network, so it can be tested
// against a saved response.
func ParseGold(body []byte, source string, fetchedAt time.Time, log *slog.Logger) (domain.GoldBoard, error) {
	if log == nil {
		log = slog.Default()
	}
	var payload goldResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return domain.GoldBoard{}, fmt.Errorf("%s: cannot decode response: %w", source, err)
	}
	if !payload.Success {
		return domain.GoldBoard{}, fmt.Errorf("%s: response reports success=false", source)
	}
	if len(payload.Prices) == 0 {
		return domain.GoldBoard{}, fmt.Errorf("%s: %w", source, domain.ErrNoQuotes)
	}

	quotes := make([]domain.GoldQuote, 0, len(payload.Prices))
	skipped := 0
	for _, code := range orderedCodes(payload.Prices) {
		entry := payload.Prices[code]
		name := entry.Name
		if pretty, ok := displayNames[code]; ok {
			name = pretty
		}
		quote, err := domain.NewGoldQuote(domain.GoldQuote{
			Code:       code,
			Name:       name,
			Buy:        entry.Buy,
			Sell:       entry.Sell,
			ChangeBuy:  entry.ChangeBuy,
			ChangeSell: entry.ChangeSell,
			Currency:   domain.Currency(entry.Currency),
		})
		if err != nil {
			// One bad row shouldn't blank the command, but it usually means the payload
			// changed.
			log.Warn("skipping a gold quote", "source", source, "error", err)
			skipped++
			continue
		}
		quotes = append(quotes, quote)
	}
	if len(quotes) == 0 {
		return domain.GoldBoard{}, fmt.Errorf("%s: every quote was rejected: %w", source, domain.ErrNoQuotes)
	}
	if skipped > 0 {
		log.Warn("some gold quotes were unusable", "source", source, "skipped", skipped)
	}
	return domain.NewGoldBoard(quotes, goldTimestamp(payload), source, fetchedAt)
}

// goldTimestamp resolves when the prices were set. `timestamp` is a plain
// Unix epoch; `date` and `time` are the same instant already in GMT+7.
// Reading the epoch as UTC is off by seven hours.
func goldTimestamp(payload goldResponse) time.Time {
	if payload.Timestamp > 0 {
		return time.Unix(payload.Timestamp, 0).In(domain.Location())
	}
	if payload.Date != "" && payload.Time != "" {
		if parsed, err := time.ParseInLocation("2006-01-02 15:04",
			payload.Date+" "+payload.Time, domain.Location()); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// orderedCodes lists the payload's keys in display order, appending anything
// unrecognised so a new brand shows up rather than disappearing.
func orderedCodes(prices map[string]goldEntry) []string {
	out := make([]string, 0, len(prices))
	seen := make(map[string]bool, len(prices))
	for _, code := range displayOrder {
		if _, ok := prices[code]; ok {
			out = append(out, code)
			seen[code] = true
		}
	}
	extra := make([]string, 0)
	for code := range prices {
		if !seen[code] {
			extra = append(extra, code)
		}
	}
	sortStrings(extra)
	return append(out, extra...)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
