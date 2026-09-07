import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { CommandInteractionOptionResolver } from "discord.js";

import { fromInteraction } from "../src/commands/interaction.js";
import { slashCommands } from "../src/commands/registry.js";

/**
 * These build a real `CommandInteractionOptionResolver` from the option
 * payload Discord actually sends, rather than a mock.
 *
 * That is the whole point. The bug these tests exist for — `getString` on an
 * option registered as an Integer — is invisible to a mock, because a mock
 * returns whatever the test author expected. Only the real resolver enforces
 * the type it was given.
 */
const T = { Subcommand: 1, String: 3, Integer: 4 } as const;

type Option = { name: string; type: number; value?: unknown; options?: Option[] };

function resolver(options: Option[]): CommandInteractionOptionResolver {
  // The constructor is not part of the public typings, but the class is
  // exported and this is exactly what discord.js does internally.
  return new (CommandInteractionOptionResolver as unknown as new (
    client: unknown,
    options: Option[],
    resolved: unknown,
  ) => CommandInteractionOptionResolver)({}, options, {});
}

const sub = (name: string, options: Option[] = []): Option[] => [
  { name, type: T.Subcommand, options },
];

describe("/thongke", () => {
  // The command that was broken: `ngay` is an Integer here and a String under
  // /thongke ngay. Reading both with getString threw before deferReply, so
  // Discord showed "The application did not respond".
  it("reads tanso's integer window and string grouping", () => {
    const parsed = fromInteraction(
      "thongke",
      resolver(
        sub("tanso", [
          { name: "kieu", type: T.String, value: "cham" },
          { name: "ngay", type: T.Integer, value: 90 },
        ]),
      ),
    );
    assert.deepEqual(parsed.args, ["thongke", "tanso", "cham", "90"]);
  });

  it("reads tanso without a window", () => {
    const parsed = fromInteraction(
      "thongke",
      resolver(sub("tanso", [{ name: "kieu", type: T.String, value: "dau" }])),
    );
    assert.deepEqual(parsed.args, ["thongke", "tanso", "dau"]);
  });

  // The second bug, which the review did not catch: the old loop looked for
  // ngay, thang and kieu, so `so` was dropped and the command answered
  // "cần một số hai chữ số" no matter what was typed.
  it("keeps lo's number", () => {
    const parsed = fromInteraction(
      "thongke",
      resolver(sub("lo", [{ name: "so", type: T.String, value: "27" }])),
    );
    assert.deepEqual(parsed.args, ["thongke", "lo", "27"]);
  });

  it("reads ngay and db, whose options are strings", () => {
    assert.deepEqual(
      fromInteraction("thongke", resolver(sub("ngay", [
        { name: "ngay", type: T.String, value: "03/09/2026" },
      ]))).args,
      ["thongke", "ngay", "03/09/2026"],
    );
    assert.deepEqual(
      fromInteraction("thongke", resolver(sub("db", [
        { name: "thang", type: T.String, value: "08/2026" },
      ]))).args,
      ["thongke", "db", "08/2026"],
    );
  });

  it("passes the option-free subcommands through", () => {
    for (const name of ["logan", "degan", "kho"]) {
      assert.deepEqual(
        fromInteraction("thongke", resolver(sub(name))).args,
        ["thongke", name],
      );
    }
  });
});

describe("top-level commands", () => {
  it("reads xsmb with and without a date", () => {
    assert.deepEqual(
      fromInteraction("xsmb", resolver([
        { name: "ngay", type: T.String, value: "03/09/2026" },
      ])).args,
      ["03/09/2026"],
    );
    assert.deepEqual(fromInteraction("xsmb", resolver([])).args, []);
  });

  it("reads bieudo's string code and integer window", () => {
    assert.deepEqual(
      fromInteraction("bieudo", resolver([
        { name: "ma", type: T.String, value: "SJC" },
        { name: "ngay", type: T.Integer, value: 60 },
      ])).args,
      ["SJC", "60"],
    );
  });

  it("keeps the private replies private", () => {
    assert.equal(fromInteraction("thongbao", resolver([])).ephemeral, true);
    assert.equal(fromInteraction("huongdan", resolver([])).ephemeral, true);
    assert.equal(fromInteraction("xsmb", resolver([])).ephemeral, false);
  });
});

/**
 * The guard against this whole class of bug: walk the registry, and for every
 * declared option read it with the getter matching its declared type. Anything
 * the parser reads with the wrong getter throws here rather than in front of a
 * user.
 */
describe("registry and parser agree on every option type", () => {
  it("parses a payload built from the registry itself", () => {
    const sample = (type: number, name: string): unknown =>
      type === T.Integer ? 30 : name === "so" ? "27" : "x";

    for (const command of slashCommands()) {
      const c = command as { name: string; options?: Option[] };
      const options = c.options ?? [];

      const subcommands = options.filter((o) => o.type === T.Subcommand);
      const payloads = subcommands.length
        ? subcommands.map((s) => [
            {
              name: s.name,
              type: T.Subcommand,
              options: (s.options ?? []).map((o) => ({
                ...o,
                value: sample(o.type, o.name),
              })),
            },
          ])
        : [options.map((o) => ({ ...o, value: sample(o.type, o.name) }))];

      for (const payload of payloads) {
        assert.doesNotThrow(
          () => fromInteraction(c.name, resolver(payload as Option[])),
          `/${c.name} ${(payload[0] as Option)?.name ?? ""}`,
        );
      }
    }
  });
});
