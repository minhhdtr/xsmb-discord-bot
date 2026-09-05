package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Tails are the last two digits of each prize. Reducing in SQL keeps twenty
// years of rows on the database side.
const (
	// loTails expands every prize of every draw into one row per tail.
	loTails = `SELECT draw_date, right(n, 2) AS lo FROM draws, unnest(numbers) AS n`
	// deTails takes only the special prize, which is the first element.
	deTails = `SELECT draw_date, right(numbers[1], 2) AS lo FROM draws`
)

// LatestDraw is the newest day held.
func (p *Postgres) LatestDraw(ctx context.Context) (time.Time, bool, error) {
	var newest sql.NullTime
	if err := p.db.QueryRowContext(ctx, `SELECT max(draw_date) FROM draws`).Scan(&newest); err != nil {
		return time.Time{}, false, fmt.Errorf("select latest draw: %w", err)
	}
	if !newest.Valid {
		return time.Time{}, false, nil
	}
	return domain.DayOf(newest.Time), true, nil
}

// LoGan ranks tails by how long they have gone unseen.
func (p *Postgres) LoGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	return p.gan(ctx, loTails, limit)
}

// DeGan does the same for the special prize alone.
func (p *Postgres) DeGan(ctx context.Context, limit int) ([]domain.Gan, error) {
	return p.gan(ctx, deTails, limit)
}

// gan computes current and record droughts in one pass. A gap of n days
// between appearances is n-1 days absent: the 1st then the 5th is three
// days, not four.
func (p *Postgres) gan(ctx context.Context, tails string, limit int) ([]domain.Gan, error) {
	if limit <= 0 {
		limit = 12
	}
	query := fmt.Sprintf(`
WITH tails AS (%s),
seen AS (SELECT DISTINCT lo, draw_date FROM tails),
gaps AS (
    SELECT lo, draw_date,
           draw_date - lag(draw_date) OVER (PARTITION BY lo ORDER BY draw_date) AS gap
    FROM seen
),
records AS (
    SELECT lo,
           coalesce(max(gap), 0) - 1 AS record,
           (array_agg(draw_date ORDER BY gap DESC NULLS LAST))[1] AS record_end
    FROM gaps GROUP BY lo
),
latest AS (SELECT max(draw_date) AS day FROM draws)
SELECT s.lo, max(s.draw_date) AS last_seen,
       (SELECT day FROM latest) - max(s.draw_date) AS days,
       greatest(r.record, 0), r.record_end
FROM seen s JOIN records r ON r.lo = s.lo
GROUP BY s.lo, r.record, r.record_end
ORDER BY days DESC, s.lo
LIMIT $1`, tails)

	rows, err := p.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("select gan: %w", err)
	}
	defer rows.Close()

	var out []domain.Gan
	for rows.Next() {
		var (
			g         domain.Gan
			lastSeen  time.Time
			recordEnd sql.NullTime
		)
		if err := rows.Scan(&g.Number, &lastSeen, &g.Days, &g.Record, &recordEnd); err != nil {
			return nil, fmt.Errorf("scan gan: %w", err)
		}
		g.LastSeen = domain.DayOf(lastSeen)
		if recordEnd.Valid {
			g.RecordEnd = domain.DayOf(recordEnd.Time)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Frequency counts appearances over the last days draws.
func (p *Postgres) Frequency(ctx context.Context, days int) ([]domain.Frequency, error) {
	rows, err := p.db.QueryContext(ctx, fmt.Sprintf(`
WITH latest AS (SELECT max(draw_date) AS day FROM draws),
window_days AS (
    SELECT draw_date FROM draws
    WHERE $1 <= 0 OR draw_date > (SELECT day FROM latest) - $1::int
),
tails AS (%s)
SELECT t.lo, count(*) AS hits, count(DISTINCT t.draw_date) AS days
FROM tails t JOIN window_days w ON w.draw_date = t.draw_date
GROUP BY t.lo ORDER BY hits DESC, t.lo`, loTails), days)
	if err != nil {
		return nil, fmt.Errorf("select frequency: %w", err)
	}
	defer rows.Close()

	var out []domain.Frequency
	for rows.Next() {
		var f domain.Frequency
		if err := rows.Scan(&f.Number, &f.Hits, &f.Days); err != nil {
			return nil, fmt.Errorf("scan frequency: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// profileWindows are the spans Profile reports, in order.
var profileWindows = []struct {
	Label string
	Days  int // 0 means the whole archive
}{{"30 ngày", 30}, {"90 ngày", 90}, {"365 ngày", 365}, {"Toàn kho", 0}}

// Profile gathers one number's history in one query. The version with a
// query per figure scanned the unnested archive eleven times, and paid that
// per number asked about.
func (p *Postgres) Profile(ctx context.Context, lo string) (domain.Profile, error) {
	const query = `
WITH latest AS (SELECT max(draw_date) AS day FROM draws),
mine AS MATERIALIZED (
    SELECT draw_date, count(*)::int AS hits
    FROM draws, unnest(numbers) AS n
    WHERE right(n, 2) = $1
    GROUP BY draw_date
),
gaps AS (
    SELECT draw_date, draw_date - lag(draw_date) OVER (ORDER BY draw_date) AS gap FROM mine
)
SELECT
    (SELECT count(*) FROM draws),
    (SELECT day FROM latest),
    (SELECT coalesce(sum(hits), 0) FROM mine),
    (SELECT count(*) FROM mine),
    (SELECT min(draw_date) FROM mine),
    (SELECT max(draw_date) FROM mine),
    (SELECT max(gap) - 1 FROM gaps),
    (SELECT (array_agg(draw_date ORDER BY gap DESC NULLS LAST))[1] FROM gaps),
    (SELECT coalesce(sum(hits), 0) FROM mine WHERE draw_date > (SELECT day FROM latest) - 30),
    (SELECT count(*) FROM mine  WHERE draw_date > (SELECT day FROM latest) - 30),
    (SELECT count(*) FROM draws WHERE draw_date > (SELECT day FROM latest) - 30),
    (SELECT coalesce(sum(hits), 0) FROM mine WHERE draw_date > (SELECT day FROM latest) - 90),
    (SELECT count(*) FROM mine  WHERE draw_date > (SELECT day FROM latest) - 90),
    (SELECT count(*) FROM draws WHERE draw_date > (SELECT day FROM latest) - 90),
    (SELECT coalesce(sum(hits), 0) FROM mine WHERE draw_date > (SELECT day FROM latest) - 365),
    (SELECT count(*) FROM mine  WHERE draw_date > (SELECT day FROM latest) - 365),
    (SELECT count(*) FROM draws WHERE draw_date > (SELECT day FROM latest) - 365)`

	var (
		archive                     int
		newest, first, last, recEnd sql.NullTime
		totalHits, totalDays        sql.NullInt64
		record                      sql.NullInt64
		w                           [9]sql.NullInt64
	)
	if err := p.db.QueryRowContext(ctx, query, lo).Scan(
		&archive, &newest, &totalHits, &totalDays, &first, &last, &record, &recEnd,
		&w[0], &w[1], &w[2], &w[3], &w[4], &w[5], &w[6], &w[7], &w[8],
	); err != nil {
		return domain.Profile{}, fmt.Errorf("select profile: %w", err)
	}

	profile := domain.Profile{Number: lo, Gan: domain.Gan{Number: lo}, Archive: archive}
	if archive == 0 {
		return profile, nil
	}
	if last.Valid && newest.Valid {
		profile.Gan.LastSeen = domain.DayOf(last.Time)
		profile.Gan.Days = daysApart(profile.Gan.LastSeen, domain.DayOf(newest.Time))
	}
	if first.Valid {
		profile.First = domain.DayOf(first.Time)
	}
	if record.Valid && record.Int64 > 0 {
		profile.Gan.Record = int(record.Int64)
	}
	if recEnd.Valid {
		profile.Gan.RecordEnd = domain.DayOf(recEnd.Time)
	}
	// Completed intervals only; the run in progress has no end yet.
	if totalDays.Int64 > 1 && first.Valid && last.Valid {
		span := daysApart(domain.DayOf(first.Time), domain.DayOf(last.Time))
		profile.AvgCycle = float64(span) / float64(totalDays.Int64-1)
	}

	for i, spec := range profileWindows {
		window := domain.Window{Label: spec.Label, Days: spec.Days}
		if spec.Days == 0 {
			window.Hits = int(totalHits.Int64)
			window.Draws = int(totalDays.Int64)
			window.Total = archive
		} else {
			window.Hits = int(w[i*3].Int64)
			window.Draws = int(w[i*3+1].Int64)
			window.Total = int(w[i*3+2].Int64)
		}
		profile.Windows = append(profile.Windows, window)
	}

	recent, err := p.recentDays(ctx, lo, domain.RecentDays)
	if err != nil {
		return domain.Profile{}, err
	}
	profile.Recent = recent
	return profile, nil
}

// recentDays lists the last n draws with the number's hit count on each, zero
// included. The date filter keeps this to an index range scan over n rows
// rather than another pass over the archive.
func (p *Postgres) recentDays(ctx context.Context, lo string, n int) ([]domain.DayHit, error) {
	rows, err := p.db.QueryContext(ctx, `
WITH latest AS (SELECT max(draw_date) AS day FROM draws),
recent AS (
    SELECT draw_date FROM draws WHERE draw_date > (SELECT day FROM latest) - $2::int
),
mine AS (
    SELECT draw_date, count(*)::int AS hits
    FROM draws, unnest(numbers) AS n
    WHERE right(n, 2) = $1 AND draw_date > (SELECT day FROM latest) - $2::int
    GROUP BY draw_date
)
SELECT r.draw_date, coalesce(m.hits, 0)
FROM recent r LEFT JOIN mine m ON m.draw_date = r.draw_date
ORDER BY r.draw_date`, lo, n)
	if err != nil {
		return nil, fmt.Errorf("select recent days: %w", err)
	}
	defer rows.Close()

	var out []domain.DayHit
	for rows.Next() {
		var (
			day  time.Time
			hits int
		)
		if err := rows.Scan(&day, &hits); err != nil {
			return nil, fmt.Errorf("scan recent day: %w", err)
		}
		out = append(out, domain.DayHit{Day: domain.DayOf(day), Hits: hits})
	}
	return out, rows.Err()
}

// daysApart counts whole calendar days between two days.
func daysApart(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

// SpecialMonth lists one month of special prizes.
func (p *Postgres) SpecialMonth(ctx context.Context, year int, month time.Month) ([]domain.SpecialDay, error) {
	first, last := domain.MonthRange(year, month)
	rows, err := p.db.QueryContext(ctx,
		`SELECT draw_date, numbers[1] FROM draws
		 WHERE draw_date BETWEEN $1 AND $2 ORDER BY draw_date`,
		domain.FormatISO(first), domain.FormatISO(last))
	if err != nil {
		return nil, fmt.Errorf("select special month: %w", err)
	}
	defer rows.Close()

	var out []domain.SpecialDay
	for rows.Next() {
		var (
			day     time.Time
			special string
		)
		if err := rows.Scan(&day, &special); err != nil {
			return nil, fmt.Errorf("scan special month: %w", err)
		}
		out = append(out, domain.SpecialDay{
			Day: domain.DayOf(day), Special: special, De: domain.Lo(special)})
	}
	return out, rows.Err()
}
