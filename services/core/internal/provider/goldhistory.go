package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// History fetches up to days of daily quotes for one gold type. The docs and
// the live endpoint disagree about the response shape, which is why
// ParseGoldHistory tries several.
func (v *VangToday) History(ctx context.Context, code string, days int) (domain.GoldSeries, error) {
	if code == "" {
		return domain.GoldSeries{}, fmt.Errorf("%s: no gold type given", v.Name())
	}
	if days < 1 {
		days = 1
	}
	if days > domain.MaxHistoryDays {
		days = domain.MaxHistoryDays
	}

	endpoint, err := url.Parse(v.url)
	if err != nil {
		return domain.GoldSeries{}, fmt.Errorf("%s: bad url: %w", v.Name(), err)
	}
	query := endpoint.Query()
	query.Set("type", code)
	query.Set("days", strconv.Itoa(days))
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return domain.GoldSeries{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xsmb-discord-bot/1.0 (+discord)")

	resp, err := v.client.Do(req)
	if err != nil {
		return domain.GoldSeries{}, fmt.Errorf("%s: %w", v.Name(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.GoldSeries{}, fmt.Errorf("%s: HTTP %d", v.Name(), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return domain.GoldSeries{}, fmt.Errorf("%s: %w", v.Name(), err)
	}
	return ParseGoldHistory(body, code, v.Name())
}

// historyRow is the union of every field name seen or documented. Decoding is
// permissive; the domain constructor is what's strict.
type historyRow struct {
	TypeCode   string      `json:"type_code"`
	Code       string      `json:"code"`
	Name       string      `json:"name"`
	Currency   string      `json:"currency"`
	Buy        json.Number `json:"buy"`
	Sell       json.Number `json:"sell"`
	UpdateTime json.Number `json:"update_time"`
	Timestamp  json.Number `json:"timestamp"`
	Time       string      `json:"time"`
	Date       string      `json:"date"`
	Day        string      `json:"day"`
}

// historyDay is the shape the live endpoint returns:
//
//	{"date":"2026-08-22","prices":{"DOHNL":{"buy":...,"sell":...}}}
type historyDay struct {
	Date      string                `json:"date"`
	Day       string                `json:"day"`
	Timestamp json.Number           `json:"timestamp"`
	Prices    map[string]historyRow `json:"prices"`
	Data      map[string]historyRow `json:"data"`
}

// historyEnvelope covers every container the rows have been seen inside.
type historyEnvelope struct {
	Success  *bool           `json:"success"`
	Type     string          `json:"type"`
	Data     json.RawMessage `json:"data"`
	History  json.RawMessage `json:"history"`
	Prices   json.RawMessage `json:"prices"`
	Series   json.RawMessage `json:"series"`
	Code     string          `json:"type_code"`
	Name     string          `json:"name"`
	Currency string          `json:"currency"`
}

// ParseGoldHistory turns a history payload into a series.
func ParseGoldHistory(body []byte, code, source string) (domain.GoldSeries, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return domain.GoldSeries{}, fmt.Errorf("%s: empty response", source)
	}

	// A bare array is possible too, so try that before looking for a wrapper.
	if strings.HasPrefix(trimmed, "[") {
		if series, ok := firstUsable(json.RawMessage(body), code, "", "", source); ok {
			return series, nil
		}
		return domain.GoldSeries{}, fmt.Errorf(
			"%s: no usable rows in the array for %s", source, code)
	}

	var envelope historyEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return domain.GoldSeries{}, fmt.Errorf("%s: cannot decode response: %w", source, err)
	}
	if envelope.Success != nil && !*envelope.Success {
		return domain.GoldSeries{}, fmt.Errorf("%s: response reports success=false", source)
	}
	if code == "" {
		code = envelope.Type
	}

	for _, candidate := range []json.RawMessage{
		envelope.History, envelope.Data, envelope.Series, envelope.Prices,
	} {
		if len(candidate) == 0 {
			continue
		}
		if series, ok := firstUsable(candidate, code, envelope.Name, envelope.Currency, source); ok {
			return series, nil
		}
	}
	return domain.GoldSeries{}, fmt.Errorf(
		"%s: no daily rows found in the response; got %s", source, shapeOf(trimmed))
}

// firstUsable runs every extractor over one container and returns the first
// series that survives validation.
func firstUsable(raw json.RawMessage, code, name, currency, source string) (domain.GoldSeries, bool) {
	for _, extract := range extractors {
		rows := extract(raw, code)
		if len(rows) == 0 {
			continue
		}
		series, err := buildSeries(rows, code, name, currency, source, "")
		if err == nil {
			return series, true
		}
	}
	return domain.GoldSeries{}, false
}

// Tried in turn until one yields a usable series. Live shape first; the
// others are what the docs describe.
var extractors = []func(json.RawMessage, string) []historyRow{
	nestedRows, flatRows, keyedRows,
}

// nestedRows reads days whose quotes sit inside a per-code object.
func nestedRows(raw json.RawMessage, code string) []historyRow {
	var days []historyDay
	if err := json.Unmarshal(raw, &days); err != nil {
		var byDate map[string]historyDay
		if err := json.Unmarshal(raw, &byDate); err != nil {
			return nil
		}
		for key, day := range byDate {
			if day.Date == "" && looksLikeDate(key) {
				day.Date = key
			}
			days = append(days, day)
		}
	}
	out := make([]historyRow, 0, len(days))
	for _, day := range days {
		quotes := day.Prices
		if len(quotes) == 0 {
			quotes = day.Data
		}
		row, ok := pickQuote(quotes, code)
		if !ok {
			continue
		}
		if row.Date == "" {
			row.Date = firstNonEmpty(day.Date, day.Day)
		}
		if row.Timestamp == "" {
			row.Timestamp = day.Timestamp
		}
		out = append(out, row)
	}
	return out
}

// pickQuote takes the requested code out of a day's quote map, falling back to
// the only entry when the map holds exactly one.
func pickQuote(quotes map[string]historyRow, code string) (historyRow, bool) {
	for key, row := range quotes {
		if strings.EqualFold(key, code) {
			row.Code = key
			return row, true
		}
	}
	if len(quotes) == 1 {
		for key, row := range quotes {
			row.Code = key
			return row, true
		}
	}
	return historyRow{}, false
}

// flatRows reads days that carry their prices directly.
func flatRows(raw json.RawMessage, _ string) []historyRow {
	var rows []historyRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	return rows
}

// keyedRows reads an object keyed by date.
func keyedRows(raw json.RawMessage, _ string) []historyRow {
	var byDate map[string]historyRow
	if err := json.Unmarshal(raw, &byDate); err != nil {
		return nil
	}
	out := make([]historyRow, 0, len(byDate))
	for key, row := range byDate {
		if row.Date == "" && row.Day == "" && looksLikeDate(key) {
			row.Date = key
		}
		out = append(out, row)
	}
	return out
}

func buildSeries(rows []historyRow, code, name, currency, source, raw string) (domain.GoldSeries, error) {
	points := make([]domain.GoldPoint, 0, len(rows))
	for _, row := range rows {
		if rowCode := firstNonEmpty(row.TypeCode, row.Code); rowCode != "" && !strings.EqualFold(rowCode, code) {
			continue // a multi-type payload; keep only the type asked for
		}
		if name == "" {
			name = row.Name
		}
		if currency == "" {
			currency = row.Currency
		}
		day, ok := rowDay(row)
		if !ok {
			continue
		}
		buy := number(row.Buy)
		if buy <= 0 {
			continue
		}
		points = append(points, domain.GoldPoint{Day: day, Buy: buy, Sell: number(row.Sell)})
	}
	if len(points) == 0 {
		return domain.GoldSeries{}, fmt.Errorf(
			"%s: found rows but none usable for %s; got %s", source, code, shapeOf(raw))
	}
	if currency == "" {
		currency = string(domain.VND)
		if strings.EqualFold(code, "XAUUSD") {
			currency = string(domain.USD)
		}
	}
	if pretty, ok := displayNames[strings.ToUpper(code)]; ok {
		name = pretty
	}
	return domain.NewGoldSeries(code, name, domain.Currency(currency), points)
}

// rowDay resolves a row's calendar day from whichever field carries it.
func rowDay(row historyRow) (time.Time, bool) {
	for _, epoch := range []json.Number{row.UpdateTime, row.Timestamp} {
		if seconds := int64(number(epoch)); seconds > 0 {
			return domain.DayOf(time.Unix(seconds, 0).In(domain.Location())), true
		}
	}
	for _, text := range []string{row.Date, row.Day} {
		if text == "" {
			continue
		}
		for _, layout := range []string{"2006-01-02", "02/01/2006", "2006/01/02", time.RFC3339} {
			if parsed, err := time.ParseInLocation(layout, text, domain.Location()); err == nil {
				return domain.DayOf(parsed), true
			}
		}
		// "2026-08-21 14:00" and similar: the date half is enough.
		if len(text) >= 10 {
			if parsed, err := time.ParseInLocation("2006-01-02", text[:10], domain.Location()); err == nil {
				return domain.DayOf(parsed), true
			}
		}
	}
	return time.Time{}, false
}

func looksLikeDate(s string) bool {
	if len(s) < 8 {
		return false
	}
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 6
}

func number(n json.Number) float64 {
	value, err := n.Float64()
	if err != nil {
		return 0
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// shapeOf names the keys an unexpected payload actually had, so a failure
// says more than "cannot parse".
func shapeOf(raw string) string {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		if len(raw) > 120 {
			raw = raw[:120] + "..."
		}
		return "non-object payload: " + raw
	}
	keys := make([]string, 0, len(probe))
	for key := range probe {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return "object with keys [" + strings.Join(keys, " ") + "]"
}
