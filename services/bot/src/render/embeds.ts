import type { APIEmbed } from "discord.js";

/**
 * Colours separate one kind of message from another at a glance, before any
 * text is read.
 *
 * `spin` matters most. A made-up board holds 27 numbers laid out exactly like a
 * real result, so the colour and the heading are the only things stopping a
 * screenshot of one being taken for the other. In the Go bot this constant was
 * accidentally set to the same value as `stats`, which quietly undid half of
 * that; it is deliberately unlike everything else here.
 */
export const colour = {
  query: 0x2b6cb0,
  announce: 0xd69e2e,
  problem: 0x9b2c2c,
  stats: 0x6b46c1,
  gold: 0xb7791f,
  spin: 0xb83280,
} as const;

/** Goes under every statistic. These figures describe what has happened, not
 * what will. */
export const statsFooter =
  "Thống kê mô tả · mỗi kỳ quay độc lập với các kỳ trước";

/**
 * Reply is what a command produces.
 *
 * `frames` are later states of the same message, shown one after another by
 * editing it. The command layer builds them all up front and the Discord layer
 * does the waiting, so nothing here needs a client — which is also what makes
 * the whole thing testable without a gateway connection.
 */
export interface Reply {
  embed: APIEmbed;
  /** PNG attachments, currently only the gold chart. */
  files?: { name: string; data: Buffer }[];
  frames?: APIEmbed[];
  /** Gap between frames. See `spinPace` for why it is not one second. */
  paceMs?: number;
}

/** Anything that is not a result: an empty archive, a date nobody can parse,
 * a source that will not answer. */
export function notice(title: string, body: string, problem = false): APIEmbed {
  return {
    title,
    description: body,
    color: problem ? colour.problem : colour.query,
  };
}

export function reply(embed: APIEmbed): Reply {
  return { embed };
}
