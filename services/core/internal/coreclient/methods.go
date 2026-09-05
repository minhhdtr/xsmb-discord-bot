package coreclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// The wire shapes. Deliberately private and deliberately duplicated from
// httpapi's: sharing one set of structs between the two ends would make a
// renamed field invisible, and this client exists partly to prove the contract
// survives being read by something that does not share the server's memory.
type (
	wireDraw struct {
		Date      string   `json:"date"`
		Special   string   `json:"special"`
		Numbers   []string `json:"numbers"`
		Source    string   `json:"source"`
		FetchedAt *string  `json:"fetched_at"`
	}

	wireGan struct {
		Number    string  `json:"number"`
		Days      int     `json:"days"`
		Record    int     `json:"record"`
		LastSeen  *string `json:"last_seen"`
		RecordEnd *string `json:"record_end"`
	}

	wireFrequency struct {
		Days    int `json:"days"`
		Entries []struct {
			Number string `json:"number"`
			Hits   int    `json:"hits"`
			Days   int    `json:"days"`
		} `json:"entries"`
	}

	wireProfile struct {
		Number  string  `json:"number"`
		Gan     wireGan `json:"gan"`
		Windows []struct {
			Label string `json:"label"`
			Days  int    `json:"days"`
			Hits  int    `json:"hits"`
		} `json:"windows"`
		Recent []struct {
			Date string `json:"date"`
			Hits int    `json:"hits"`
		} `json:"recent"`
		AvgCycle float64 `json:"avg_cycle"`
		First    *string `json:"first"`
		Archive  int     `json:"archive"`
	}

	wireSpecialMonth struct {
		Days []struct {
			Date    string `json:"date"`
			Special string `json:"special"`
			De      string `json:"de"`
		} `json:"days"`
	}

	wireArchive struct {
		Draws    int     `json:"draws"`
		Absences int     `json:"absences"`
		Channels int     `json:"channels"`
		Earliest *string `json:"earliest"`
		Latest   *string `json:"latest"`
	}

	wireSpin struct {
		Numbers []string `json:"numbers"`
		Order   []int    `json:"order"`
	}

	wireSubscription struct {
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
	}

	wireChanged struct {
		Created bool `json:"created"`
		Removed bool `json:"removed"`
	}
)

// isoDay parses a date from the wire. A malformed one is a broken contract
// rather than a runtime condition, so it surfaces as an error rather than a
// zero time that would quietly render as year 1.
func isoDay(raw string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", raw, domain.Location())
}

func optionalDay(raw *string) time.Time {
	if raw == nil {
		return time.Time{}
	}
	day, err := isoDay(*raw)
	if err != nil {
		return time.Time{}
	}
	return day
}

func (w wireDraw) toDomain() (domain.Draw, error) {
	day, err := isoDay(w.Date)
	if err != nil {
		return domain.Draw{}, fmt.Errorf("core sent date %q: %w", w.Date, err)
	}
	prizes, err := domain.NewPrizes(w.Numbers)
	if err != nil {
		return domain.Draw{}, fmt.Errorf("core sent a board this client rejects: %w", err)
	}
	draw := domain.Draw{Date: day, Prizes: prizes, Source: w.Source}
	if w.FetchedAt != nil {
		if at, err := time.Parse(time.RFC3339, *w.FetchedAt); err == nil {
			draw.FetchedAt = at.In(domain.Location())
		}
	}
	return draw, nil
}

func (w wireGan) toDomain() domain.Gan {
	return domain.Gan{
		Number:    w.Number,
		Days:      w.Days,
		Record:    w.Record,
		LastSeen:  optionalDay(w.LastSeen),
		RecordEnd: optionalDay(w.RecordEnd),
	}
}

// --- draws ---

func (c *Client) Latest(ctx context.Context) (domain.Draw, error) {
	var wire wireDraw
	if err := c.do(ctx, "GET", "/v1/draws/latest", nil, &wire); err != nil {
		return domain.Draw{}, err
	}
	return wire.toDomain()
}

func (c *Client) Draw(ctx context.Context, day time.Time) (domain.Draw, error) {
	var wire wireDraw
	path := "/v1/draws/" + domain.FormatISO(day)
	if err := c.do(ctx, "GET", path, nil, &wire); err != nil {
		return domain.Draw{}, err
	}
	return wire.toDomain()
}

// --- statistics ---

func (c *Client) gan(ctx context.Context, scope string, limit int) ([]domain.Gan, error) {
	var wire []wireGan
	path := "/v1/stats/gan" + query(url.Values{
		"scope": {scope}, "limit": {itoa(limit)},
	})
	if err := c.do(ctx, "GET", path, nil, &wire); err != nil {
		return nil, err
	}
	out := make([]domain.Gan, 0, len(wire))
	for _, g := range wire {
		out = append(out, g.toDomain())
	}
	return out, nil
}

func (c *Client) LoGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	return c.gan(ctx, "lo", limit)
}

func (c *Client) DeGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	return c.gan(ctx, "de", limit)
}

func (c *Client) Frequency(ctx context.Context, days int) ([]domain.Frequency, error) {
	var wire wireFrequency
	path := "/v1/stats/frequency" + query(url.Values{"days": {itoa(days)}})
	if err := c.do(ctx, "GET", path, nil, &wire); err != nil {
		return nil, err
	}
	out := make([]domain.Frequency, 0, len(wire.Entries))
	for _, f := range wire.Entries {
		out = append(out, domain.Frequency{Number: f.Number, Hits: f.Hits, Days: f.Days})
	}
	return out, nil
}

func (c *Client) Profile(ctx context.Context, lo string) (domain.Profile, error) {
	var wire wireProfile
	if err := c.do(ctx, "GET", "/v1/stats/number/"+url.PathEscape(lo), nil, &wire); err != nil {
		return domain.Profile{}, err
	}
	out := domain.Profile{
		Number:   wire.Number,
		Gan:      wire.Gan.toDomain(),
		AvgCycle: wire.AvgCycle,
		First:    optionalDay(wire.First),
		Archive:  wire.Archive,
	}
	for _, win := range wire.Windows {
		out.Windows = append(out.Windows, domain.Window{Label: win.Label, Days: win.Days, Hits: win.Hits})
	}
	for _, hit := range wire.Recent {
		day, err := isoDay(hit.Date)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("core sent date %q: %w", hit.Date, err)
		}
		out.Recent = append(out.Recent, domain.DayHit{Day: day, Hits: hit.Hits})
	}
	return out, nil
}

func (c *Client) SpecialMonth(ctx context.Context, year int, month time.Month) ([]domain.SpecialDay, error) {
	var wire wireSpecialMonth
	path := "/v1/stats/special-month" + query(url.Values{
		"month": {fmt.Sprintf("%04d-%02d", year, int(month))},
	})
	if err := c.do(ctx, "GET", path, nil, &wire); err != nil {
		return nil, err
	}
	out := make([]domain.SpecialDay, 0, len(wire.Days))
	for _, d := range wire.Days {
		day, err := isoDay(d.Date)
		if err != nil {
			return nil, fmt.Errorf("core sent date %q: %w", d.Date, err)
		}
		out = append(out, domain.SpecialDay{Day: day, Special: d.Special, De: d.De})
	}
	return out, nil
}

func (c *Client) Archive(ctx context.Context) (domain.Archive, error) {
	var wire wireArchive
	if err := c.do(ctx, "GET", "/v1/archive", nil, &wire); err != nil {
		return domain.Archive{}, err
	}
	return domain.Archive{
		Draws:    wire.Draws,
		Absences: wire.Absences,
		Channels: wire.Channels,
		Earliest: optionalDay(wire.Earliest),
		Latest:   optionalDay(wire.Latest),
	}, nil
}

// --- spins ---

func (c *Client) Spin(ctx context.Context) (domain.Spin, error) {
	var wire wireSpin
	if err := c.do(ctx, "POST", "/v1/spins", nil, &wire); err != nil {
		return domain.Spin{}, err
	}
	return domain.SpinFrom(wire.Numbers, wire.Order), nil
}

// --- subscriptions ---

func (c *Client) Subscriptions(ctx context.Context) ([]domain.Subscription, error) {
	var wire []wireSubscription
	if err := c.do(ctx, "GET", "/v1/subscriptions", nil, &wire); err != nil {
		return nil, err
	}
	out := make([]domain.Subscription, 0, len(wire))
	for _, s := range wire {
		out = append(out, domain.Subscription{ChannelID: s.ChannelID, GuildID: s.GuildID})
	}
	return out, nil
}

func (c *Client) Subscribe(ctx context.Context, guildID, channelID string) (bool, error) {
	var wire wireChanged
	body := map[string]string{"guild_id": guildID}
	path := "/v1/subscriptions/" + url.PathEscape(channelID)
	if err := c.do(ctx, "PUT", path, body, &wire); err != nil {
		return false, err
	}
	return wire.Created, nil
}

func (c *Client) Unsubscribe(ctx context.Context, channelID string) (bool, error) {
	var wire wireChanged
	path := "/v1/subscriptions/" + url.PathEscape(channelID)
	if err := c.do(ctx, "DELETE", path, nil, &wire); err != nil {
		return false, err
	}
	return wire.Removed, nil
}

// ClaimAnnouncement reports whether this caller may post. A conflict is an
// ordinary outcome here, not a failure, so it becomes false rather than an
// error - the caller's job is to stay quiet, not to handle a problem.
func (c *Client) ClaimAnnouncement(ctx context.Context, day time.Time, channelID string) (bool, error) {
	path := fmt.Sprintf("/v1/announcements/%s/%s",
		domain.FormatISO(day), url.PathEscape(channelID))
	err := c.do(ctx, "POST", path, nil, nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrAlreadyClaimed):
		return false, nil
	default:
		return false, err
	}
}

func (c *Client) ReleaseAnnouncement(ctx context.Context, day time.Time, channelID string) error {
	path := fmt.Sprintf("/v1/announcements/%s/%s",
		domain.FormatISO(day), url.PathEscape(channelID))
	return c.do(ctx, "DELETE", path, nil, nil)
}

// --- gold ---

type (
	wireGoldQuote struct {
		Code       string  `json:"code"`
		Name       string  `json:"name"`
		Buy        float64 `json:"buy"`
		Sell       float64 `json:"sell"`
		ChangeBuy  float64 `json:"change_buy"`
		ChangeSell float64 `json:"change_sell"`
		Currency   string  `json:"currency"`
	}

	wireGoldBoard struct {
		Quotes    []wireGoldQuote `json:"quotes"`
		UpdatedAt *string         `json:"updated_at"`
		FetchedAt *string         `json:"fetched_at"`
		Source    string          `json:"source"`
	}

	wireGoldSeries struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Currency string `json:"currency"`
		Points   []struct {
			Date string  `json:"date"`
			Buy  float64 `json:"buy"`
			Sell float64 `json:"sell"`
		} `json:"points"`
	}
)

func optionalStamp(raw *string) time.Time {
	if raw == nil {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return time.Time{}
	}
	return at.In(domain.Location())
}

func (c *Client) GoldBoard(ctx context.Context) (domain.GoldBoard, error) {
	var wire wireGoldBoard
	if err := c.do(ctx, "GET", "/v1/gold", nil, &wire); err != nil {
		return domain.GoldBoard{}, err
	}
	quotes := make([]domain.GoldQuote, 0, len(wire.Quotes))
	for _, q := range wire.Quotes {
		quotes = append(quotes, domain.GoldQuote{
			Code: q.Code, Name: q.Name, Buy: q.Buy, Sell: q.Sell,
			ChangeBuy: q.ChangeBuy, ChangeSell: q.ChangeSell,
			Currency: domain.Currency(q.Currency),
		})
	}
	// Rebuilt through the same constructor the server used, so a board that
	// would be rejected here would have been rejected there too.
	return domain.NewGoldBoard(quotes, optionalStamp(wire.UpdatedAt),
		wire.Source, optionalStamp(wire.FetchedAt))
}

func (c *Client) GoldHistory(ctx context.Context, code string, days int) (domain.GoldSeries, error) {
	var wire wireGoldSeries
	path := "/v1/gold/" + url.PathEscape(code) + "/history" +
		query(url.Values{"days": {itoa(days)}})
	if err := c.do(ctx, "GET", path, nil, &wire); err != nil {
		return domain.GoldSeries{}, err
	}
	points := make([]domain.GoldPoint, 0, len(wire.Points))
	for _, p := range wire.Points {
		day, err := isoDay(p.Date)
		if err != nil {
			return domain.GoldSeries{}, fmt.Errorf("core sent date %q: %w", p.Date, err)
		}
		points = append(points, domain.GoldPoint{Day: day, Buy: p.Buy, Sell: p.Sell})
	}
	return domain.NewGoldSeries(wire.Code, wire.Name,
		domain.Currency(wire.Currency), points)
}
