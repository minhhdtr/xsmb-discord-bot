-- Draws are immutable once published, so there is no updated_at and no
-- history table. The cardinality check mirrors domain.TotalNumbers: the
-- database refuses a partial result just as the domain layer does.
CREATE TABLE IF NOT EXISTS draws (
    draw_date  date        PRIMARY KEY,
    numbers    text[]      NOT NULL CHECK (cardinality(numbers) = 27),
    source     text        NOT NULL,
    fetched_at timestamptz NOT NULL DEFAULT now()
);

-- Days the source positively confirmed have no result. Written only on a
-- definitive 404 for a day already in the past - never on a network failure,
-- which is the whole reason this is a separate table and not a null column.
CREATE TABLE IF NOT EXISTS absences (
    draw_date  date        PRIMARY KEY,
    checked_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
    channel_id text        PRIMARY KEY,
    guild_id   text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- One row per (day, channel) that has been announced. The primary key is the
-- idempotency guard: a restart at 18:40 cannot double-post, and neither can a
-- second bot instance.
CREATE TABLE IF NOT EXISTS announcements (
    draw_date  date        NOT NULL,
    channel_id text        NOT NULL,
    posted_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (draw_date, channel_id)
);
