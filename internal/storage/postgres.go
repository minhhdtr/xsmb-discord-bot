package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Postgres is the production Store.
type Postgres struct {
	db *sql.DB
}

// OpenPostgres connects, verifies the connection and runs migrations.
func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	store := &Postgres{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the pool.
func (p *Postgres) Close() error { return p.db.Close() }

// migrate applies each embedded file once, in filename order. Each runs in its
// own transaction with its bookkeeping row.
func (p *Postgres) migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := p.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := p.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

// Draw reads one day. Stored numbers go back through the domain constructor,
// so corrupt data is caught on the way out.
func (p *Postgres) Draw(ctx context.Context, day time.Time) (domain.Draw, bool, error) {
	var (
		date    time.Time
		numbers []string
		source  string
		fetched time.Time
	)
	err := p.db.QueryRowContext(ctx,
		`SELECT draw_date, numbers, source, fetched_at FROM draws WHERE draw_date = $1`,
		domain.FormatISO(day),
	).Scan(&date, pq.Array(&numbers), &source, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Draw{}, false, nil
	}
	if err != nil {
		return domain.Draw{}, false, fmt.Errorf("select draw: %w", err)
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		return domain.Draw{}, false, fmt.Errorf("stored draw %s is corrupt: %w", domain.FormatISO(day), err)
	}
	return domain.Draw{
		Date:      domain.DayOf(date),
		Prizes:    prizes,
		Source:    source,
		FetchedAt: fetched.In(domain.Location()),
	}, true, nil
}

// SaveDraw is idempotent - results never change once published.
func (p *Postgres) SaveDraw(ctx context.Context, draw domain.Draw) error {
	if !draw.Prizes.Valid() {
		return fmt.Errorf("refusing to store %s: %w", domain.FormatISO(draw.Date), domain.ErrIncomplete)
	}
	fetched := draw.FetchedAt
	if fetched.IsZero() {
		fetched = time.Now().In(domain.Location())
	}
	if _, err := p.db.ExecContext(ctx,
		`INSERT INTO draws (draw_date, numbers, source, fetched_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (draw_date) DO NOTHING`,
		domain.FormatISO(draw.Date), pq.Array(draw.Prizes.Numbers()), draw.Source, fetched,
	); err != nil {
		return fmt.Errorf("insert draw: %w", err)
	}
	// A day that turns out to have a result is no longer an absence.
	if _, err := p.db.ExecContext(ctx,
		`DELETE FROM absences WHERE draw_date = $1`, domain.FormatISO(draw.Date)); err != nil {
		return fmt.Errorf("clear absence: %w", err)
	}
	return nil
}

// IsAbsent reports a confirmed empty day.
func (p *Postgres) IsAbsent(ctx context.Context, day time.Time) (bool, error) {
	var exists bool
	if err := p.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM absences WHERE draw_date = $1)`, domain.FormatISO(day),
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("select absence: %w", err)
	}
	return exists, nil
}

// MarkAbsent records a confirmed empty day.
func (p *Postgres) MarkAbsent(ctx context.Context, day time.Time) error {
	if _, err := p.db.ExecContext(ctx,
		`INSERT INTO absences (draw_date) VALUES ($1) ON CONFLICT (draw_date) DO NOTHING`,
		domain.FormatISO(day),
	); err != nil {
		return fmt.Errorf("insert absence: %w", err)
	}
	return nil
}

// Subscribe registers a channel for announcements.
func (p *Postgres) Subscribe(ctx context.Context, guildID, channelID string) (bool, error) {
	result, err := p.db.ExecContext(ctx,
		`INSERT INTO subscriptions (channel_id, guild_id) VALUES ($1, $2)
		 ON CONFLICT (channel_id) DO NOTHING`, channelID, guildID)
	if err != nil {
		return false, fmt.Errorf("insert subscription: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// Unsubscribe removes a channel.
func (p *Postgres) Unsubscribe(ctx context.Context, channelID string) (bool, error) {
	result, err := p.db.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE channel_id = $1`, channelID)
	if err != nil {
		return false, fmt.Errorf("delete subscription: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// Subscriptions lists every registered channel.
func (p *Postgres) Subscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT channel_id, guild_id, created_at FROM subscriptions ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("select subscriptions: %w", err)
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ChannelID, &s.GuildID, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ClaimAnnouncement wins the race to post a given day to a given channel.
func (p *Postgres) ClaimAnnouncement(ctx context.Context, day time.Time, channelID string) (bool, error) {
	result, err := p.db.ExecContext(ctx,
		`INSERT INTO announcements (draw_date, channel_id) VALUES ($1, $2)
		 ON CONFLICT (draw_date, channel_id) DO NOTHING`,
		domain.FormatISO(day), channelID)
	if err != nil {
		return false, fmt.Errorf("claim announcement: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// ReleaseAnnouncement undoes a claim after a failed send.
func (p *Postgres) ReleaseAnnouncement(ctx context.Context, day time.Time, channelID string) error {
	if _, err := p.db.ExecContext(ctx,
		`DELETE FROM announcements WHERE draw_date = $1 AND channel_id = $2`,
		domain.FormatISO(day), channelID); err != nil {
		return fmt.Errorf("release announcement: %w", err)
	}
	return nil
}

// Stats summarises the archive.
func (p *Postgres) Stats(ctx context.Context) (Stats, error) {
	var (
		s                Stats
		earliest, latest sql.NullTime
	)
	if err := p.db.QueryRowContext(ctx,
		`SELECT count(*), min(draw_date), max(draw_date) FROM draws`,
	).Scan(&s.Draws, &earliest, &latest); err != nil {
		return Stats{}, fmt.Errorf("select draw stats: %w", err)
	}
	if earliest.Valid {
		s.Earliest = domain.DayOf(earliest.Time)
	}
	if latest.Valid {
		s.Latest = domain.DayOf(latest.Time)
	}
	if err := p.db.QueryRowContext(ctx, `SELECT count(*) FROM absences`).Scan(&s.Absences); err != nil {
		return Stats{}, fmt.Errorf("select absence stats: %w", err)
	}
	if err := p.db.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions`).Scan(&s.Channels); err != nil {
		return Stats{}, fmt.Errorf("select channel stats: %w", err)
	}
	return s, nil
}
