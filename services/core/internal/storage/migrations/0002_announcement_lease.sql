-- A claim is taken before the message is sent and given back if the send
-- fails. That leaves one gap: if the process dies between the two, the row
-- stays and the day is never announced to that channel.
--
-- The cheap fix is to treat the row as a lease rather than a permanent fact.
-- sent_at separates "someone is trying" from "it went out", and a claim that
-- is still unsent after the lease expires can be taken over.
--
-- This is deliberately not a four-state machine with attempt counters and
-- stored errors. Exactly-once delivery to an API with no idempotency key is
-- not available at any price; the choice is only which way to be wrong, and
-- the two columns below cover the failure that actually loses a day.
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS sent_at timestamptz;

-- Rows written before this migration were only ever inserted after a send was
-- attempted, so treat them as sent rather than leaving them reclaimable.
UPDATE announcements SET sent_at = posted_at WHERE sent_at IS NULL;
