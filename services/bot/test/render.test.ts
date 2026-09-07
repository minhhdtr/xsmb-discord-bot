import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { colour } from "../src/render/embeds.js";
import { formatMonth, formatVN, parseDate, parseMonth, weekdayVN } from "../src/render/dates.js";
import { SPIN_LABEL, spinTable } from "../src/render/stats.js";
import { width } from "../src/render/text.js";
import { DateTime } from "luxon";

const today = DateTime.fromISO("2026-09-04", { zone: "Asia/Ho_Chi_Minh" });

describe("dates", () => {
  it("reads the forms people actually type", () => {
    assert.equal(parseDate("03/09/2026", today), "2026-09-03");
    assert.equal(parseDate("3-9-2026", today), "2026-09-03");
    assert.equal(parseDate("03/09", today), "2026-09-03");
    assert.equal(parseDate("3", today), "2026-09-03");
    assert.equal(parseDate("03/09/26", today), "2026-09-03");
    assert.equal(parseDate("2026-09-03", today), "2026-09-03");
    assert.equal(parseDate("hôm qua", today), "2026-09-03");
  });

  // Day first, always. Guessing is this side's job precisely so core never has
  // to; getting it backwards would silently return the wrong month.
  it("reads day before month", () => {
    assert.equal(parseDate("01/09/2026", today), "2026-09-01");
    assert.equal(parseDate("09/01/2026", today), "2026-01-09");
  });

  it("rejects what it cannot read rather than guessing", () => {
    for (const bad of ["", "abc", "32/13/2026", "///"]) {
      assert.equal(parseDate(bad, today), null, `parseDate(${bad})`);
    }
  });

  it("reads months", () => {
    assert.equal(parseMonth("09/2026", today), "2026-09");
    assert.equal(parseMonth("9/2026", today), "2026-09");
    assert.equal(parseMonth("2026-09", today), "2026-09");
    assert.equal(parseMonth("9", today), "2026-09");
    assert.equal(parseMonth("13/2026", today), null);
    assert.equal(formatMonth("2026-09"), "09/2026");
  });

  it("names weekdays", () => {
    assert.equal(weekdayVN("2026-09-04"), "Thứ Sáu");
    assert.equal(weekdayVN("2026-09-06"), "Chủ Nhật");
    assert.equal(formatVN("2026-09-04"), "04/09/2026");
  });
});

describe("spin colour", () => {
  // A made-up board is laid out exactly like a real one, so the colour is one
  // of the few things keeping a screenshot from being read as a result. In the
  // Go bot this was accidentally set to the same value as the stats colour.
  it("is unlike every other colour", () => {
    const values = Object.values(colour);
    assert.equal(new Set(values).size, values.length);
  });
});

describe("spinTable", () => {
  const full = [
    "50066", "71152", "34677", "11336",
    "31123", "91287", "35599", "38872", "70150", "30636",
    "8795", "2876", "3557", "6896",
    "1372", "8325", "0353", "0211", "7949", "0185",
    "053", "732", "243",
    "06", "14", "74", "88",
  ];

  // The columns must not shift as the board fills, or it reads as a glitch and
  // gets blamed on Discord.
  it("keeps every row the same width at every step", () => {
    const widths = (step: number) => {
      const cells: (string | undefined)[] = new Array(27).fill(undefined);
      // Reveal order: prize one through seven, special last.
      const order = [...Array(26).keys()].map((n) => n + 1).concat(0);
      for (const at of order.slice(0, step)) cells[at] = full[at];
      return spinTable(cells).split("\n").map(width);
    };
    const finished = widths(27);
    for (let step = 0; step <= 27; step++) {
      assert.deepEqual(widths(step), finished, `bước ${step}`);
    }
  });

  it("pads a blank cell to the width of the number that goes there", () => {
    const blank = spinTable(new Array(27).fill(undefined)).split("\n");
    assert.ok(blank[0]?.includes("·····"), "đặc biệt 5 chữ số");
    assert.ok(blank.at(-1)?.includes("··"), "giải bảy 2 chữ số");
  });

  it("labels the board so a screenshot cannot pass as a result", () => {
    assert.equal(SPIN_LABEL, "QUAY THỬ");
  });
});

describe("slash registry", () => {
  it("derives example dates from the clock, not the source file", async () => {
    const { slashCommands } = await import("../src/commands/registry.js");
    const morning = DateTime.fromISO("2026-09-04T09:00", { zone: "Asia/Ho_Chi_Minh" }) as DateTime<true>;
    const evening = DateTime.fromISO("2026-09-04T19:00", { zone: "Asia/Ho_Chi_Minh" }) as DateTime<true>;

    const dayOption = (at: DateTime<true>) => {
      const xsmb = slashCommands(at).find((c) => "name" in c && c.name === "xsmb");
      const options = (xsmb as { options?: { description?: string }[] }).options ?? [];
      return options[0]?.description ?? "";
    };

    // Before the mark the newest result is yesterday's, so the example is a
    // date that actually has one.
    assert.match(dayOption(morning), /03\/09\/2026/);
    assert.match(dayOption(evening), /04\/09\/2026/);

    // The example month is the previous one; an example matching the default
    // teaches nothing.
    const thongke = slashCommands(morning).find((c) => "name" in c && c.name === "thongke");
    const subs = (thongke as { options?: { name?: string; options?: { description?: string }[] }[] }).options ?? [];
    const db = subs.find((s) => s.name === "db");
    assert.match(db?.options?.[0]?.description ?? "", /08\/2026/);
  });

  // Discord rejects a description over 100 characters, and a dynamic date
  // makes the length easy to lose track of.
  it("keeps every description inside Discord's limit", async () => {
    const { slashCommands } = await import("../src/commands/registry.js");
    const walk = (nodes: unknown[]): void => {
      for (const node of nodes) {
        const n = node as { description?: string; options?: unknown[] };
        if (n.description !== undefined) {
          assert.ok([...n.description].length <= 100, n.description);
        }
        if (n.options) walk(n.options);
      }
    };
    walk(slashCommands());
  });
});
