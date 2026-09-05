import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { padEnd, padStart, width } from "../src/render/text.js";

describe("width", () => {
  // The reason this module exists. The same word, composed and decomposed,
  // measures differently with .length and identically with width().
  it("counts characters, not UTF-16 units", () => {
    const composed = "Đặc biệt".normalize("NFC");
    const decomposed = "Đặc biệt".normalize("NFD");

    assert.notEqual(decomposed.length, composed.length);
    assert.equal(width(decomposed), width(composed));
    assert.equal(width(composed), 8);
  });

  it("counts every Vietnamese heading the board uses", () => {
    const headings = ["Đặc biệt", "Nhất", "Nhì", "Ba", "Tư", "Năm", "Sáu", "Bảy"];
    const expected = [8, 4, 3, 2, 2, 3, 3, 3];
    assert.deepEqual(headings.map(width), expected);
    assert.deepEqual(headings.map((h) => width(h.normalize("NFD"))), expected);
  });
});

describe("padEnd", () => {
  it("lines up columns whatever form the input arrives in", () => {
    const rows = ["Đặc biệt", "Nhất", "Bảy"].map((label) =>
      padEnd(label.normalize("NFD"), 9) + "12345",
    );
    const offsets = rows.map((row) => [...row].findIndex((c) => c >= "0" && c <= "9"));
    assert.equal(new Set(offsets).size, 1);
    assert.equal(offsets[0], 9);
  });

  it("never truncates something already too wide", () => {
    assert.equal(padEnd("Đặc biệt", 3), "Đặc biệt");
  });
});

describe("padStart", () => {
  it("right-aligns numbers", () => {
    assert.equal(padStart("7", 4), "   7");
    assert.equal(padStart("1234", 4), "1234");
  });
});
