import type { APIEmbed } from "discord.js";
import type { GoldBoard, Profile } from "../core/client.js";
import { colour, notice, statsFooter } from "./embeds.js";
import { formatStamp, formatVN } from "./dates.js";
import { changeDong, changeLabel, decimal, dong } from "./numbers.js";
import { fence, padEnd, padStart } from "./text.js";

/** Discord allows 25 fields; this leaves room for the ones added around them. */
const MAX_GOLD_FIELDS = 20;

/**
 * A gold board.
 *
 * Fields rather than a code block: a row is a label and two numbers, which fits
 * inline fields without truncation and reflows on a phone. The draw tables go
 * the other way for the opposite reason — there, alignment is the point.
 */
export function goldEmbed(board: GoldBoard): APIEmbed {
  if (board.quotes.length === 0) {
    return notice("Lỗi", "Không có dữ liệu giá vàng.", true);
  }

  const world = board.quotes.find((q) => q.currency === "USD");
  const domestic = board.quotes.filter((q) => q.currency !== "USD");

  return {
    title:
      "🥇 Giá vàng · " +
      (formatStamp(board.updated_at) || "không rõ thời điểm"),
    color: colour.gold,
    ...(world
      ? {
          description: `**${decimal(world.buy, 1)}** USD/oz  ${changeLabel(world.change_buy ?? 0, 1)}`,
        }
      : {}),
    fields: domestic.slice(0, MAX_GOLD_FIELDS).map((q) => ({
      name: q.name,
      value: goldValue(q),
      inline: true,
    })),
    footer: {
      text: `Nguồn ${board.source} · mỗi lượng · lấy lúc ${formatStamp(board.fetched_at)}`,
    },
  };
}

function goldValue(quote: GoldBoard["quotes"][number]): string {
  const lines = [`Mua **${dong(quote.buy)}**`, `Bán **${dong(quote.sell ?? 0)}**`];
  const buy = quote.change_buy ?? 0;
  const sell = quote.change_sell ?? 0;

  if (buy !== 0 || sell !== 0) {
    // The two sides have matched in every row seen so far, but show both if
    // they ever disagree rather than picking one and hiding the difference.
    lines.push(
      buy === sell
        ? changeDong(buy)
        : `mua ${changeDong(buy)} · bán ${changeDong(sell)}`,
    );
  }
  return lines.join("\n");
}

/** Everything about one two-digit number. */
export function profileEmbed(profile: Profile): APIEmbed {
  if (profile.archive === 0) {
    return notice("Chưa có dữ liệu", "Kho chưa có kỳ quay nào để thống kê.");
  }

  const head: string[] = [];
  if (!profile.gan.last_seen) {
    head.push("Chưa từng về trong kho");
  } else {
    head.push(
      padEnd("Lần cuối về", 14) +
        `${formatVN(profile.gan.last_seen)} · ${profile.gan.days} ngày trước`,
    );
    head.push(padEnd("Đang gan", 14) + `${profile.gan.days} ngày`);
    if (profile.gan.record > 0 && profile.gan.record_end) {
      head.push(
        padEnd("Kỷ lục gan", 14) +
          `${profile.gan.record} ngày, hết ngày ${formatVN(profile.gan.record_end)}`,
      );
    }
    if (profile.avg_cycle > 0) {
      head.push(padEnd("Chu kỳ TB", 14) + `${decimal(profile.avg_cycle, 1)} ngày`);
    }
    if (profile.first) {
      head.push(padEnd("Về lần đầu", 14) + formatVN(profile.first));
    }
  }

  const windows = profile.windows.map(
    (w) =>
      padEnd(w.label, 9) +
      ` ${padStart(String(w.hits), 4)} nháy · ${padStart(String(w.draws), 4)}/${w.total} kỳ`,
  );

  const fields: NonNullable<APIEmbed["fields"]> = [
    { name: "Tần suất", value: fence(windows.join("\n")) },
  ];

  const strip = recentStrip(profile.recent);
  if (strip) fields.push(strip);

  if (profile.gan.new_record) {
    fields.push({
      name: "Đang vượt kỷ lục",
      value: `Gan ${profile.gan.days} ngày, dài hơn kỷ lục cũ ${profile.gan.record} ngày.`,
    });
  }

  return {
    title: `🔎 Lô ${profile.number}`,
    color: colour.stats,
    description: fence(head.join("\n")),
    fields,
    footer: { text: statsFooter },
  };
}

/**
 * One character per recent draw: the hit count, or a dot for a miss.
 *
 * Concrete and checkable, unlike a ranking — a reader can count the dots and
 * see for themselves what the gan figure above is claiming.
 */
function recentStrip(
  recent: Profile["recent"],
): { name: string; value: string } | null {
  if (recent.length === 0) return null;

  const strip = recent
    .map((day) => (day.hits <= 0 ? "·" : day.hits > 9 ? "+" : String(day.hits)))
    .join("");

  const first = recent[0];
  const last = recent.at(-1);
  if (!first || !last) return null;

  return {
    name: `${recent.length} kỳ gần nhất · ${formatVN(first.date)} → ${formatVN(last.date)}`,
    value: fence(strip) + "· = không về · số = số nháy",
  };
}

/** The help text, built from the prefix in use so the examples are copyable. */
export function helpEmbed(prefix: string, goldPrefix: string): APIEmbed {
  const lines = (rows: string[]) => rows.join("\n");

  return {
    title: "📖 Hướng dẫn",
    color: colour.query,
    fields: [
      {
        name: "Kết quả",
        value: lines([
          "`/xsmb` — kỳ mới nhất",
          "`/xsmb ngay:03/09/2026` — một ngày cụ thể",
          "`/quaythu` — quay thử một bảng cho vui: số ngẫu nhiên, không lưu vào kho",
          "`/thongbao trangthai:bật` — bật thông báo hằng ngày cho kênh này",
        ]),
      },
      {
        name: "Thống kê",
        value: lines([
          "`/thongke ngay` — phân tích một kỳ: kép, nháy, câm, chạm",
          "`/thongke db thang:08/2026` — bảng giải đặc biệt cả tháng",
          "`/thongke logan` — lô lâu chưa về nhất, kèm kỷ lục gan",
          "`/thongke degan` — như logan nhưng chỉ tính giải đặc biệt",
          "`/thongke tanso kieu:chạm` — gom 100 số thành 10 ô",
          "`/thongke lo so:27` — hồ sơ đầy đủ của một số",
          "`/thongke kho` — kho dữ liệu đang có gì",
        ]),
      },
      {
        name: "Giá vàng",
        value: lines([
          "`/gold` — bảng giá hiện tại",
          "`/bieudo` — biểu đồ lịch sử giá; ô `ma` có gợi ý mã",
        ]),
      },
      {
        name: "Dạng gõ tay",
        value: lines([
          `\`${prefix}\`, \`${prefix} 03/09/2026\`, \`${prefix} logan\`, \`${prefix} db 08/2026\``,
          `\`${prefix} sub\` / \`${prefix} unsub\`, \`${prefix} status\`, \`${prefix} lo 88\``,
          `\`${goldPrefix}\`, \`${goldPrefix} chart\`, \`${goldPrefix} chart VNGSJC 30\``,
        ]),
      },
    ],
    footer: {
      text: "Thống kê mô tả · mỗi kỳ quay độc lập với các kỳ trước",
    },
  };
}
