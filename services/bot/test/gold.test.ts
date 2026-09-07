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
  it("reads the code and the window in either order", () => {
    assert.deepEqual(parseChartArgs(["SJC", "90"]), { code: "SJC", days: 90 });
    assert.deepEqual(parseChartArgs(["90", "sjc"]), { code: "SJC", days: 90 });
    assert.deepEqual(parseChartArgs(["pnj"]), { code: "PNJ", days: 30 });
    assert.deepEqual(parseChartArgs([]), { code: "SJC", days: 30 });
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
    const { embed } = await goldRouter().goldChart(["SJC", "365"]);
    assert.equal(embed.title, "Không hợp lệ");
    assert.match(embed.description ?? "", /2 đến 30/);
  });

  withGold("attaches the chart core drew", async () => {
    const reply = await goldRouter().goldChart(["SJC", "30"]);
    assert.equal(reply.files?.length, 1);
    assert.equal(reply.files?.[0]?.name, "chart.png");
    // A real PNG, not an error body rendered as bytes.
    assert.deepEqual([...(reply.files?.[0]?.data.subarray(0, 4) ?? [])], [0x89, 0x50, 0x4e, 0x47]);
    assert.equal(reply.embed.image?.url, "attachment://chart.png");
  });
});
