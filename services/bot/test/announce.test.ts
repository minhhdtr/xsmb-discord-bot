import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { DateTime } from "luxon";
import type { APIEmbed } from "discord.js";

import { Core } from "../src/core/client.js";
import { Announcer, msUntilNextRun, worthRetrying } from "../src/commands/announce.js";
import { CoreError } from "../src/core/client.js";

const baseUrl = process.env.CORE_URL ?? "http://127.0.0.1:8099";
const required = process.env.CORE_REQUIRED === "1";
const reachable = await fetch(baseUrl + "/healthz", { signal: AbortSignal.timeout(1000) })
  .then((r) => r.ok)
  .catch(() => false);

if (required && !reachable) {
  throw new Error(`CORE_REQUIRED=1 but no core answered at ${baseUrl}`);
}

const withCore = (name: string, fn: () => Promise<void>) =>
  it(name, { skip: reachable ? false : `no core at ${baseUrl}` }, fn);

const zone = "Asia/Ho_Chi_Minh";
const at = (iso: string) => DateTime.fromISO(iso, { zone });

describe("msUntilNextRun", () => {
  it("waits until 18:35 today when the mark is still ahead", () => {
    assert.equal(msUntilNextRun(at("2026-09-04T09:00")), 9.5 * 3600_000 + 5 * 60_000);
  });

  // The reason it is computed from the clock rather than by adding 24 hours:
  // a restart at 18:40 must wait until tomorrow, not fire at once.
  it("waits until tomorrow when the mark has passed", () => {
    const wait = msUntilNextRun(at("2026-09-04T18:40"));
    assert.equal(wait, 23 * 3600_000 + 55 * 60_000);
  });

  it("treats the mark itself as passed", () => {
    assert.ok(msUntilNextRun(at("2026-09-04T18:35")) > 23 * 3600_000);
  });
});

describe("announcing", () => {
  const collect = () => {
    const posts: { channelId: string; embed: APIEmbed }[] = [];
    return {
      posts,
      post: async (channelId: string, embed: APIEmbed) => {
        posts.push({ channelId, embed });
      },
    };
  };

  withCore("posts to every subscriber, marked as an announcement", async () => {
    const core = new Core({ baseUrl, timeoutMs: 5_000 });
    const channels = [`ann-a-${Date.now()}`, `ann-b-${Date.now()}`];
    for (const channel of channels) await core.subscribe(channel, "g1");

    const { posts, post } = collect();
    await new Announcer(core, post, { gapMs: 0 }).runFor("2026-08-20");

    const sentTo = posts.map((p) => p.channelId);
    for (const channel of channels) {
      assert.ok(sentTo.includes(channel), `thiếu ${channel}`);
    }
    // The bell, not the dice: a scheduled post has to look different from an
    // answer somebody asked for.
    assert.match(posts[0]?.embed.title ?? "", /^🔔 XSMB · /);

    for (const channel of channels) await core.unsubscribe(channel);
  });

  // What stops a restart at 18:50 putting the same result out twice.
  withCore("never posts the same day to the same channel twice", async () => {
    const core = new Core({ baseUrl, timeoutMs: 5_000 });
    const channel = `ann-once-${Date.now()}`;
    await core.subscribe(channel, "g1");

    const first = collect();
    await new Announcer(core, first.post, { gapMs: 0 }).runFor("2026-08-19");
    assert.equal(first.posts.filter((p) => p.channelId === channel).length, 1);

    const second = collect();
    await new Announcer(core, second.post, { gapMs: 0 }).runFor("2026-08-19");
    assert.equal(second.posts.filter((p) => p.channelId === channel).length, 0);

    await core.unsubscribe(channel);
  });

  // A Discord hiccup must not lose the day: the claim goes back so the next
  // run tries again.
  withCore("gives the claim back when the post fails", async () => {
    const core = new Core({ baseUrl, timeoutMs: 5_000 });
    const channel = `ann-retry-${Date.now()}`;
    await core.subscribe(channel, "g1");

    const failing = new Announcer(core, async () => {
      throw new Error("Discord said no");
    }, { gapMs: 0 });
    await failing.runFor("2026-08-18");

    const retry = collect();
    await new Announcer(core, retry.post, { gapMs: 0 }).runFor("2026-08-18");
    assert.equal(retry.posts.filter((p) => p.channelId === channel).length, 1);

    await core.unsubscribe(channel);
  });

  // Every subscription is cleared here, which is why the runner is told to
  // take one test file at a time: the files share a core, and therefore share
  // its database. Concurrency would make this wipe rows another file is in the
  // middle of asserting on.
  withCore("says nothing when no channel is subscribed", async () => {
    const core = new Core({ baseUrl, timeoutMs: 5_000 });
    for (const s of await core.subscriptions()) await core.unsubscribe(s.channel_id);

    const { posts, post } = collect();
    await new Announcer(core, post, { gapMs: 0 }).runFor("2026-08-17");
    assert.equal(posts.length, 0);
  });
});


describe("worthRetrying", () => {
  // The bug this exists for: any error that was not `not_yet` ended the
  // attempt, so core restarting at 18:35 lost the whole day.
  it("waits through anything temporary", () => {
    assert.equal(worthRetrying(new CoreError("not_yet", 404, "chưa có")), true);
    assert.equal(worthRetrying(new CoreError("internal", 500, "lỗi")), true);
    assert.equal(worthRetrying(new CoreError("upstream", 502, "nguồn")), true);
    // status 0 is fetch failing outright: DNS, refused, socket dropped.
    assert.equal(worthRetrying(new CoreError("unknown", 0, "unreachable")), true);
    assert.equal(worthRetrying(new TypeError("fetch failed")), true);
  });

  it("stops on anything that asking again cannot fix", () => {
    assert.equal(worthRetrying(new CoreError("no_draw", 404, "không quay")), false);
    assert.equal(worthRetrying(new CoreError("out_of_range", 400, "ngoài kho")), false);
    assert.equal(worthRetrying(new CoreError("bad_request", 400, "sai")), false);
  });
});

describe("the claim is a lease", () => {
  const collect = () => {
    const posts: { channelId: string; embed: APIEmbed }[] = [];
    return {
      posts,
      post: async (channelId: string, embed: APIEmbed) => {
        posts.push({ channelId, embed });
      },
    };
  };

  // A claim left unsent must not silence the channel for good. This checks the
  // half the bot controls: a successful post is recorded as sent, so nothing
  // later reclaims it.
  withCore("marks a sent announcement so it is never reclaimed", async () => {
    const core = new Core({ baseUrl, timeoutMs: 5_000 });
    const channel = `lease-${Date.now()}`;
    await core.subscribe(channel, "g1");

    const { posts, post } = collect();
    await new Announcer(core, post, { gapMs: 0 }).runFor("2026-08-20");
    assert.equal(posts.filter((p) => p.channelId === channel).length, 1);

    // Sent, so claiming again fails however long anyone waits.
    assert.equal(await core.claimAnnouncement("2026-08-20", channel), false);

    await core.unsubscribe(channel);
  });
});
