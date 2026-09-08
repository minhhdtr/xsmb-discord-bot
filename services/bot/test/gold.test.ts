import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { Core } from "../src/core/client.js";
import { Router, parseChartArgs } from "../src/commands/router.js";
import { changeDong, changeLabel, decimal, dong, round } from "../src/render/numbers.js";
import { helpEmbed } from "../src/render/gold.js";

/** Gold is off in the default fakecore, so these need the `gold` argument.
 * Set GOLD_CORE_URL to point at one. */
const goldUrl = process.env.GOLD_CORE_URL ?? "";
const goldReachable = goldUrl
  ? await fetch(goldUrl + "/healthz", { signal: AbortSignal.timeout(1000) })
      .then((r) => r.ok)
      .catch(() => false)
  : false;

const withGold = (name: string, fn: () => Promise<void>) =>
  it(name, { skip: goldReachable ? false : "no gold-enabled core (set GOLD_CORE_URL)" }, fn);

const goldRouter = () => new Router(new Core({ baseUrl: goldUrl, timeoutMs: 5_000 }));

describe("numbers", () => {
  // Prices arrive with float noise — a movement of exactly 7.6 comes through
  // as 7.599999999999454. Showing that unrounded turns a flat day into a
  // suspiciously precise one.
  it("rounds before formatting", () => {
    assert.equal(round(7.599999999999454, 1), 7.6);
    assert.equal(changeLabel(7.599999999999454, 1), "↑ 7,6");
  });

  it("uses Vietnamese separators", () => {
    assert.equal(decimal(7600, 0), "7.600");
    assert.equal(dong(8_200_000), "8.200.000₫");
  });

  // Zero gets its own glyph rather than a signed zero, which reads as an error.
  it("marks a flat movement", () => {
    assert.equal(changeLabel(0), "→ 0");
    assert.equal(changeDong(0), "→ 0₫");
    assert.equal(changeDong(-0.4), "→ 0₫");
  });

  it("shows direction", () => {
    assert.equal(changeDong(50_000), "↑ 50.000₫");
    assert.equal(changeDong(-20_000), "↓ 20.000₫");
  });
});

describe("parseChartArgs", () => {
  // Order-free for the same reason tanso is: a command nobody can remember
  // the order of is a command nobody uses.
  // A number too small to be a window used to fall through to the code
  // branch, so `!gold chart 1` meant "the gold code 1" and `!gold chart SJC 1`
  // threw the code away. Anything numeric is a window now; core rejects the
  // ones it cannot serve, with a message that says so.
  it("treats every number as a window, even an unusable one", () => {
    assert.deepEqual(parseChartArgs(["1"]), { code: "SJL1L10", days: 1 });
    assert.deepEqual(parseChartArgs(["SJC", "1"]), { code: "SJC", days: 1 });
    assert.deepEqual(parseChartArgs(["0"]), { code: "SJL1L10", days: 0 });
  });

  it("reads the code and the window in either order", () => {
    assert.deepEqual(parseChartArgs(["VNGSJC", "30"]), { code: "VNGSJC", days: 30 });
    assert.deepEqual(parseChartArgs(["30", "vngsjc"]), { code: "VNGSJC", days: 30 });
    assert.deepEqual(parseChartArgs(["dohnl"]), { code: "DOHNL", days: 30 });
  });

  // The default has to be a code the source actually knows. It was "SJC",
  // which does not exist, so a bare /bieudo failed for everyone while the
  // tests passed - the fake core answered for any code at all.
  it("defaults to a code the source knows", () => {
    assert.deepEqual(parseChartArgs([]), { code: "SJL1L10", days: 30 });
  });
});

describe("help", () => {
  it("shows the prefixes actually in use", () => {
    const embed = helpEmbed("!xs", "!vang");
    const manual = embed.fields?.find((f) => f.name === "Dạng gõ tay");
    assert.match(manual?.value ?? "", /!xs/);
    assert.match(manual?.value ?? "", /!vang/);
  });

  it("covers every command group", () => {
    const names = helpEmbed("!xsmb", "!gold").fields?.map((f) => f.name) ?? [];
    assert.deepEqual(names, ["Kết quả", "Thống kê", "Giá vàng", "Dạng gõ tay"]);
  });
});

describe("gold command", () => {
  withGold("renders a board with a field per dealer", async () => {
    const { embed } = await goldRouter().gold();
    assert.match(embed.title ?? "", /Giá vàng/);
    assert.match(embed.description ?? "", /USD\/oz/);
    assert.ok((embed.fields?.length ?? 0) >= 2);
    assert.match(embed.fields?.[0]?.value ?? "", /Mua \*\*/);
  });

  // The contract used to advertise 3650 days while the provider quietly
  // returned 30. Refusing is the honest answer: a caller asking for a year and
  // getting a month had no way to tell.
  withGold("refuses a window the source cannot fill", async () => {
    const { embed } = await goldRouter().goldChart(["VNGSJC", "365"]);
    assert.equal(embed.title, "Không hợp lệ");
    assert.match(embed.description ?? "", /2 đến 30/);
  });

  // The bare command has to work: it is what most people type.
  withGold("charts something when no code is given", async () => {
    const reply = await goldRouter().goldChart([]);
    assert.equal(reply.files?.length, 1);
  });

  // An unknown code is the caller's mistake, and used to surface as the same
  // opaque message as a source that had changed shape.
  withGold("says so when the code does not exist", async () => {
    const { embed } = await goldRouter().goldChart(["SJC"]);
    assert.notEqual(embed.image?.url, "attachment://chart.png");
  });

  withGold("attaches the chart core drew", async () => {
    const reply = await goldRouter().goldChart(["VNGSJC", "30"]);
    assert.equal(reply.files?.length, 1);
    assert.equal(reply.files?.[0]?.name, "chart.png");
    // A real PNG, not an error body rendered as bytes.
    assert.deepEqual([...(reply.files?.[0]?.data.subarray(0, 4) ?? [])], [0x89, 0x50, 0x4e, 0x47]);
    assert.equal(reply.embed.image?.url, "attachment://chart.png");
  });
});

describe("autocomplete", () => {
  // The Go bot offered these and the port dropped them; nothing noticed,
  // because an interaction the bot ignores looks the same as a slow one.
  withGold("offers the codes the source is quoting", async () => {
    const codes = await goldRouter().goldCodes("");
    assert.ok(codes.length >= 2, `chỉ có ${codes.length} mã`);
    assert.ok(codes.every((c) => c.value.length > 0 && c.name.length > 0));
    // Discord refuses more than 25.
    assert.ok(codes.length <= 25);
  });

  withGold("filters on what has been typed, by code or by name", async () => {
    const codes = await goldRouter().goldCodes("doji");
    assert.ok(codes.length >= 1);
    assert.ok(
      codes.every(
        (c) =>
          c.value.toLowerCase().includes("doji") ||
          c.name.toLowerCase().includes("doji"),
      ),
    );
  });

  // An empty dropdown is the worst outcome: the person cannot type past it.
  // With no core at all there is still something to choose from.
  it("falls back to a built-in list when core cannot answer", async () => {
    const { Core } = await import("../src/core/client.js");
    const { Router } = await import("../src/commands/router.js");
    const offline = new Router(new Core({ baseUrl: "http://127.0.0.1:1", timeoutMs: 300 }));

    const codes = await offline.goldCodes("");
    assert.ok(codes.length > 0, "danh sách dự phòng rỗng");
    // Every fallback code must be one the source would accept — the first
    // version of this list was four codes that do not exist.
    assert.ok(codes.some((c) => c.value === "SJL1L10"));
  });
});
