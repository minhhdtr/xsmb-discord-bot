import type { APIEmbed } from "discord.js";
import type {
  DayReport,
  Draw,
  Gan,
  GroupedFrequency,
  SpecialMonth,
  Spin,
} from "../core/client.js";
import { colour, notice, statsFooter } from "./embeds.js";
import { formatMonth, formatStamp, formatVN, weekdayVN } from "./dates.js";
import { fence, padEnd, padStart } from "./text.js";

/**
 * One draw. `announced` changes the colour and the icon so the scheduled 18:35
 * post stands apart from an answer somebody asked for.
 *
 * The two code blocks come from core already drawn. Which prize holds how many
 * numbers of how many digits is domain knowledge, and redoing the column
 * arithmetic here would be a second implementation of it — one that could
 * disagree, in a way that reads as a rendering glitch rather than a bug.
 */
export function drawEmbed(draw: Draw, announced = false): APIEmbed {
  return {
    title: `${announced ? "🔔" : "🎲"} XSMB · ${weekdayVN(draw.date)} ${formatVN(draw.date)}`,
    description: `**Đặc biệt · ${draw.special}**\n${fence(draw.table)}`,
    color: announced ? colour.announce : colour.query,
    fields: draw.head_tail
      ? [{ name: "Đầu đuôi", value: fence(draw.head_tail), inline: false }]
      : [],
    footer: {
      text: `Nguồn ${draw.source} · lấy lúc ${formatStamp(draw.fetched_at)}`,
    },
  };
}

/** A month of special prizes. */
export function specialMonthEmbed(month: SpecialMonth): APIEmbed {
  if (month.days.length === 0) {
    return notice(
      "Không có dữ liệu",
      `Kho chưa có kỳ quay nào trong tháng ${formatMonth(month.month)}.`,
    );
  }
  return {
    title: `🎱 Giải đặc biệt tháng ${formatMonth(month.month)}`,
    description: fence(month.table),
    color: colour.stats,
    footer: { text: `${month.days.length} kỳ · ${statsFooter}` },
  };
}

/** One draw read closely: kép, nháy, câm, and what the special touches. */
export function dayReportEmbed(report: DayReport): APIEmbed {
  const dash = (values: number[]): string =>
    values.length === 0 ? "—" : values.join(" ");

  const nhay =
    report.nhay.length === 0
      ? "—"
      : report.nhay.map((n) => `${n.number} (${nhayWord(n.hits)} nháy)`).join("\n");

  return {
    title: `📊 Phân tích XSMB · ${weekdayVN(report.date)} ${formatVN(report.date)}`,
    color: colour.stats,
    description:
      `**Đề · ${report.de}** — chạm đầu **${report.cham_dau}**, ` +
      `chạm đuôi **${report.cham_duoi}**, tổng **${report.tong_de}**\n` +
      fence(report.table),
    fields: [
      { name: "Lô kép", value: report.kep.length ? report.kep.join(" ") : "—", inline: true },
      { name: "Nháy", value: nhay, inline: true },
      {
        name: "Về nhiều nhất",
        value: `đầu ${dash(report.top_heads)} · đuôi ${dash(report.top_tails)}`,
        inline: true,
      },
      {
        name: "Câm",
        value: `đầu ${dash(report.mute_heads)} · đuôi ${dash(report.mute_tails)}`,
        inline: false,
      },
    ],
    footer: { text: statsFooter },
  };
}

/** Multiplicity read out loud, the way a person says it. */
function nhayWord(hits: number): string {
  return { 2: "hai", 3: "ba", 4: "bốn", 5: "năm" }[hits] ?? String(hits);
}

/** A drought ranking. The record column is what makes the current figure
 * readable; `*` marks a run that has passed its own record. */
export function ganEmbed(
  title: string,
  entries: Gan[],
  archive: number,
  asOf: string,
): APIEmbed {
  if (entries.length === 0) {
    return notice(
      "Chưa có dữ liệu",
      "Kho chưa có kỳ quay nào để thống kê. Chờ backfill chạy xong nhé.",
    );
  }
  const rows = entries.map((g) => {
    const mark = g.new_record ? "*" : " ";
    return (
      `${padEnd(g.number, 3)}${padStart(String(g.days), 5)}${mark}` +
      `${padStart(String(g.record), 7)}`
    );
  });
  const header = `${padEnd("Số", 3)}${padStart("Gan", 5)} ${padStart("Kỷ lục", 6)}`;

  return {
    title,
    color: colour.stats,
    description: fence([header, ...rows].join("\n")),
    footer: {
      text: `* đang phá kỷ lục · kho ${archive.toLocaleString("vi-VN")} kỳ · tính tới ${asOf} · ${statsFooter}`,
    },
  };
}

/** The hundred numbers folded into ten buckets. */
export function groupedFrequencyEmbed(
  grouped: GroupedFrequency,
  archive: number,
): APIEmbed {
  if (grouped.total === 0) {
    return notice("Chưa có dữ liệu", "Kho chưa có kỳ quay nào để thống kê.");
  }
  const window = grouped.days > 0 ? `${grouped.days} ngày` : "toàn kho";
  const note =
    `Chia đều là ${Math.round(grouped.even).toLocaleString("vi-VN")} lần mỗi ô.` +
    (grouped.overlaps
      ? " Một lô chạm hai chữ số nên được đếm ở cả hai ô, trừ lô kép."
      : "");

  return {
    title: `📊 Tần suất ${groupLabel(grouped.group)} · ${window}`,
    color: colour.stats,
    description: `${fence(grouped.table)}\n${note}`,
    footer: {
      text: `Đếm nháy · kho ${archive.toLocaleString("vi-VN")} kỳ · ${statsFooter}`,
    },
  };
}

function groupLabel(group: GroupedFrequency["group"]): string {
  return { dau: "đầu", duoi: "đuôi", tong: "tổng", cham: "chạm" }[group];
}

/** The label on every frame of a spin. */
export const SPIN_LABEL = "QUAY THỬ";

/**
 * The board after `step` reveals. Step 0 is blank.
 *
 * Rebuilt here rather than fetched per frame: core hands over the numbers and
 * the reveal order once, and slicing them is arithmetic, not a rule. Fetching
 * 27 times would also mean 27 requests for a board that never changes.
 */
export function spinEmbed(spin: Spin, step: number, table: string): APIEmbed {
  const shown = Math.max(0, Math.min(step, spin.order.length));
  const at = shown >= 1 ? spin.order[shown - 1] : undefined;

  let headline = "Đang quay…";
  if (shown >= spin.order.length) {
    headline = `**Đặc biệt · ${spin.numbers[0]}**`;
  } else if (at !== undefined) {
    headline = `**${prizeLabel(at)}** · ${spin.numbers[at]}`;
  }

  return {
    title: `🎰 ${SPIN_LABEL}`,
    color: colour.spin,
    description: `${headline}\n${fence(table)}`,
    footer: {
      text: `Số ngẫu nhiên, quay cho vui · ${shown}/${spin.order.length}`,
    },
  };
}

/** How many numbers each prize holds, and how many digits, in board order.
 * A property of the XSMB board, which is why it is a constant rather than a
 * rule fetched from core. */
const LAYOUT: readonly { label: string; count: number; digits: number }[] = [
  { label: "Đặc biệt", count: 1, digits: 5 },
  { label: "Nhất", count: 1, digits: 5 },
  { label: "Nhì", count: 2, digits: 5 },
  { label: "Ba", count: 6, digits: 5 },
  { label: "Tư", count: 4, digits: 4 },
  { label: "Năm", count: 6, digits: 4 },
  { label: "Sáu", count: 3, digits: 3 },
  { label: "Bảy", count: 4, digits: 2 },
];

function prizeLabel(index: number): string {
  let offset = 0;
  for (const tier of LAYOUT) {
    if (index < offset + tier.count) return tier.label;
    offset += tier.count;
  }
  return "";
}

/** Numbers per line, so the widest row stays inside a code block on a phone. */
const PER_ROW = [1, 1, 2, 3, 4, 3, 3, 4] as const;

/** The heading column, padded by characters rather than UTF-16 units. */
const LABEL_WIDTH = 9;

/**
 * Draws a board that is only part filled, for the spin.
 *
 * This is the one table the bot builds itself, because it is the one core
 * never sees: the reveal happens frame by frame on this side. A cell not yet
 * drawn becomes dots the exact width of the number that goes there, so the
 * columns do not jump as the board fills — which is the sort of thing that
 * looks like a glitch and gets blamed on Discord.
 */
export function spinTable(cells: readonly (string | undefined)[]): string {
  const rows: string[] = [];
  let at = 0;

  LAYOUT.forEach((tier, index) => {
    const numbers: string[] = [];
    for (let n = 0; n < tier.count; n++) {
      numbers.push(cells[at] ?? "·".repeat(tier.digits));
      at++;
    }
    const perRow = PER_ROW[index] ?? tier.count;
    for (let start = 0; start < numbers.length; start += perRow) {
      const heading = start === 0 ? tier.label : "";
      rows.push(padEnd(heading, LABEL_WIDTH) + numbers.slice(start, start + perRow).join("  "));
    }
  });
  return rows.map((row) => row.replace(/\s+$/, "")).join("\n");
}

/** Every frame of a spin, blank board first. */
export function spinFrames(spin: Spin): APIEmbed[] {
  const frames: APIEmbed[] = [];
  for (let step = 0; step <= spin.order.length; step++) {
    const cells: (string | undefined)[] = new Array(spin.numbers.length).fill(undefined);
    for (const at of spin.order.slice(0, step)) cells[at] = spin.numbers[at];
    frames.push(spinEmbed(spin, step, spinTable(cells)));
  }
  return frames;
}
