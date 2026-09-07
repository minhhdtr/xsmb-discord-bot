import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { Core } from "../src/core/client.js";
import { Router, SPIN_PACE_MS } from "../src/commands/router.js";

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

const router = () => new Router(new Core({ baseUrl, timeoutMs: 5_000 }));
const req = (args: string[], channelId = "chan-1") => ({ args, channelId });

describe("draws", () => {
  withCore("a bare command gives the newest result", async () => {
    const { embed } = await router().handle(req([]));
    assert.match(embed.title ?? "", /^🎲 XSMB · /);
    assert.match(embed.description ?? "", /\*\*Đặc biệt · \d{5}\*\*/);
    assert.equal(embed.fields?.[0]?.name, "Đầu đuôi");
  });

  withCore("a date is read day-first and handed to core as ISO", async () => {
    const { embed } = await router().handle(req(["20/08/2026"]));
    assert.match(embed.title ?? "", /20\/08\/2026/);
  });

  withCore("an unreadable date says so instead of failing", async () => {
    const { embed } = await router().handle(req(["hôm-kia-kìa"]));
    assert.equal(embed.title, "Không đọc được ngày");
  });

  // The contract keeps not_yet and no_draw apart so the bot can too: one means
  // come back later, the other means never.
  withCore("a date outside the archive gets its own message", async () => {
    const { embed } = await router().handle(req(["01/01/2030"]));
    assert.equal(embed.title, "Ngoài phạm vi");
  });
});

describe("statistics", () => {
  withCore("thongke db renders a month", async () => {
    const { embed } = await router().handle(req(["thongke", "db", "08/2026"]));
    assert.match(embed.title ?? "", /Giải đặc biệt tháng 08\/2026/);
    assert.match(embed.footer?.text ?? "", /mỗi kỳ quay độc lập/);
  });

  withCore("thongke ngay renders one draw closely", async () => {
    const { embed } = await router().handle(req(["thongke", "ngay", "20/08/2026"]));
    assert.match(embed.title ?? "", /Phân tích XSMB/);
    const names = embed.fields?.map((f) => f.name) ?? [];
    assert.deepEqual(names, ["Lô kép", "Nháy", "Về nhiều nhất", "Câm"]);
  });

  withCore("tanso accepts the window and the grouping in either order", async () => {
    for (const args of [
      ["thongke", "tanso", "30", "cham"],
      ["thongke", "tanso", "cham", "30"],
      ["thongke", "tanso", "chạm"],
    ]) {
      const { embed } = await router().handle(req(args));
      assert.match(embed.title ?? "", /Tần suất chạm/, args.join(" "));
    }
  });

  withCore("tanso rejects a parameter that is neither", async () => {
    const { embed } = await router().handle(req(["thongke", "tanso", "xyz"]));
    assert.equal(embed.title, "Không đọc được tham số");
  });

  withCore("logan shows the record column", async () => {
    const { embed } = await router().handle(req(["thongke", "logan"]));
    assert.match(embed.description ?? "", /Kỷ lục/);
  });
});

describe("spins", () => {
  withCore("builds every frame up front and locks the channel", async () => {
    const r = router();

    const first = await r.handle(req(["quaythu"], "spin-a"));
    assert.equal(first.frames?.length, 27);
    assert.equal(first.paceMs, SPIN_PACE_MS);
    assert.match(first.embed.title ?? "", /QUAY THỬ/);
    // The blank board, not the finished one.
    assert.match(first.embed.description ?? "", /·····/);

    // A second spin would interleave its edits and burn the channel's budget.
    const second = await r.handle(req(["quaythu"], "spin-a"));
    assert.equal(second.frames, undefined);
    assert.equal(second.embed.title, "Đang quay");

    const other = await r.handle(req(["quaythu"], "spin-b"));
    assert.equal(other.frames?.length, 27);
  });

  withCore("labels every frame, not only the last", async () => {
    const { embed, frames = [] } = await router().handle(req(["quaythu"], "spin-c"));
    for (const frame of [embed, ...frames]) {
      assert.match(frame.title ?? "", /QUAY THỬ/);
      assert.match(frame.footer?.text ?? "", /ngẫu nhiên/);
    }
    assert.match(frames.at(-1)?.description ?? "", /\*\*Đặc biệt · \d{5}\*\*/);
  });
});

describe("subscriptions", () => {
  withCore("says whether anything actually changed", async () => {
    const r = router();
    const channel = `sub-${Date.now()}`;

    assert.equal((await r.subscribe(req([], channel), true)).embed.title, "Đã bật thông báo");
    assert.equal((await r.subscribe(req([], channel), true)).embed.title, "Vốn đã bật");
    assert.equal((await r.subscribe(req([], channel), false)).embed.title, "Đã tắt thông báo");
    assert.equal((await r.subscribe(req([], channel), false)).embed.title, "Vốn đã tắt");
  });
});
