import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Core, CoreError } from "../src/core/client.js";
import { width } from "../src/render/text.js";

/**
 * These run against the Go handlers, not a mock.
 *
 * A mock would agree with whatever the test author believed the contract said,
 * which is the one thing not worth checking once two languages read the same
 * document. Set CORE_URL and start `go run ./cmd/fakecore` to run them.
 */
const baseUrl = process.env.CORE_URL ?? "http://127.0.0.1:8099";
const core = new Core({ baseUrl, timeoutMs: 5_000 });

/**
 * Reachability is probed once, before anything runs, because vitest needs the
 * answer to decide whether to skip rather than to run and pass.
 *
 * The first version of this file swallowed an unreachable core and let each
 * test return early, which vitest then reported as passing. That is worse than
 * no test at all: the suite stays green while checking nothing, and the day the
 * contract breaks it stays green too. Skipped tests are reported as skipped and
 * are visible in the output; a passing test that did nothing is not.
 */
const required = process.env.CORE_REQUIRED === "1";
const reachable = await fetch(baseUrl + "/healthz", {
  signal: AbortSignal.timeout(1000),
})
  .then((r) => r.ok)
  .catch(() => false);

if (required && !reachable) {
  throw new Error(
    `CORE_REQUIRED=1 but no core answered at ${baseUrl}. ` +
      "Start it with: go run ./cmd/fakecore :8099",
  );
}

const withCore = (name: string, fn: () => Promise<void>) =>
  it(name, { skip: reachable ? false : `no core at ${baseUrl}` }, fn);

describe("draws", () => {
  withCore("carries the numbers and a table already drawn", async () => {
    const draw = await core.draw("2026-08-20");

    assert.equal(draw.date, "2026-08-20");
    assert.equal((draw.numbers).length, 27);
    assert.equal((draw.tails).length, 27);
    assert.equal(draw.special, draw.numbers[0]);
    assert.equal(draw.de, draw.special.slice(-2));
  });

  // The whole reason core sends the table: the columns are already right, and
  // this asserts they survive the trip through JSON into JavaScript.
  withCore("sends a table whose columns line up", async () => {
    const draw = await core.draw("2026-08-20");
    const rows = draw.table.split("\n");

    assert.ok((rows.length) >= 8);
    const offsets = rows.map((row) => {
      const at = [...row.normalize("NFC")].findIndex((c) => c >= "0" && c <= "9");
      return width([...row.normalize("NFC")].slice(0, at).join(""));
    });
    assert.equal(new Set(offsets).size, 1);
  });

  withCore("head_tail covers all ten heads and every number", async () => {
    const draw = await core.draw("2026-08-20");
    const rows = draw.head_tail?.split("\n") ?? [];

    assert.equal((rows).length, 10);
    const counted = rows
      .map((row) => row.split("│")[1]?.trim() ?? "")
      .filter((tails) => tails !== "-")
      .reduce((total, tails) => total + tails.split(/\s+/).filter(Boolean).length, 0);
    assert.equal(counted, 27);
  });
});

describe("error codes", () => {
  // The distinction the contract exists to preserve: both are 404, and only
  // one is worth asking about again.
  withCore("keeps not_yet and no_draw apart", async () => {
    await assert.rejects(core.draw("2030-01-01"), (error: unknown) => error instanceof CoreError && error.code === "out_of_range");
  });

  withCore("reports a feature that is switched off, not an error", async () => {
    // fakecore runs without gold.
    await assert.rejects(core.goldBoard(), (error: unknown) => error instanceof CoreError && error.notConfigured);
  });
});

describe("statistics", () => {
  withCore("groups a frequency into ten buckets with a table", async () => {
    const grouped = await core.groupedFrequency("cham", 30);

    assert.equal(grouped.grouped, true);
    assert.equal((grouped.buckets).length, 10);
    assert.equal(grouped.overlaps, true);
    assert.equal((grouped.table.split("\n")).length, 10);
    // Chạm counts a number in two buckets, so the total exceeds the numbers
    // drawn. A client showing the total has to know that.
    const summed = grouped.buckets.reduce((total, b) => total + b.hits, 0);
    assert.equal(summed, grouped.total);
  });

  withCore("returns arrays rather than null for empty lists", async () => {
    const report = await core.dayReport("2026-08-20");
    assert.equal(Array.isArray(report.kep), true);
    assert.equal(Array.isArray(report.nhay), true);
    assert.equal(Array.isArray(report.mute_heads), true);
  });
});

describe("spins", () => {
  withCore("reveals the special last", async () => {
    const spin = await core.spin();

    assert.equal((spin.numbers).length, 27);
    assert.equal((spin.order).length, 27);
    assert.equal(spin.order.at(-1), 0);
    assert.equal(new Set(spin.order).size, 27);
  });
});

describe("subscriptions", () => {
  withCore("subscribe and unsubscribe report whether anything changed", async () => {
    const channel = `test-${Date.now()}`;

    assert.equal(await core.subscribe(channel, "guild-1"), true);
    assert.equal(await core.subscribe(channel, "guild-1"), false);
    assert.equal(await core.unsubscribe(channel), true);
    assert.equal(await core.unsubscribe(channel), false);
  });

  // Losing the race is an ordinary outcome, so it is false rather than a throw.
  withCore("an announcement can be claimed once", async () => {
    const channel = `claim-${Date.now()}`;

    assert.equal(await core.claimAnnouncement("2026-08-20", channel), true);
    assert.equal(await core.claimAnnouncement("2026-08-20", channel), false);
    await core.releaseAnnouncement("2026-08-20", channel);
    assert.equal(await core.claimAnnouncement("2026-08-20", channel), true);
  });
});

// Reached by no command, so nothing else would notice if the contract moved
// under it. A client that covers the contract should be exercised across it.
describe("ungrouped frequency", () => {
  withCore("lists one entry per number that appeared", async () => {
    const listing = await core.frequency(30);
    assert.equal(listing.grouped, false);
    assert.ok(Array.isArray(listing.entries));
    for (const entry of listing.entries) {
      assert.match(entry.number, /^[0-9]{2}$/);
      assert.ok(entry.hits >= entry.days, `${entry.number}: hits < days`);
    }
  });
});
