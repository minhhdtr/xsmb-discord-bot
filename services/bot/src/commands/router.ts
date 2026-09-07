import { Core, CoreError } from "../core/client.js";
import type { Grouping } from "../core/client.js";
import { notice, reply, type Reply } from "../render/embeds.js";
import { formatVN, now, parseDate, parseMonth } from "../render/dates.js";
import { goldEmbed, helpEmbed, profileEmbed } from "../render/gold.js";
import {
  dayReportEmbed,
  drawEmbed,
  ganEmbed,
  groupedFrequencyEmbed,
  specialMonthEmbed,
  spinFrames,
} from "../render/stats.js";

/** A command, however it arrived. Slash options and prefix words are flattened
 * to the same shape so there is one handler per command rather than two. */
export interface Request {
  args: string[];
  channelId: string;
  guildId?: string;
}

/**
 * Gap between spin frames.
 *
 * Discord allows roughly five message edits per five seconds in a channel, so
 * one a second sits on the limit. 1.2s keeps a margin, and a spin taking about
 * half a minute reads better than one that scrolls past. Discord say not to
 * hard code their limits, since they change; this is the one place to adjust.
 */
export const SPIN_PACE_MS = 1_200;

const DEFAULT_WINDOW = 30;
const GAN_LIMIT = 10;

export class Router {
  /** Channels with a spin in flight, and when it should be done. The
   * reservation expires on its own, so nothing has to hand a lock back across
   * the layer that does the waiting. */
  readonly #spins = new Map<string, number>();

  constructor(
    private readonly core: Core,
    private readonly prefix = "!xsmb",
    private readonly goldPrefix = "!gold",
  ) {}

  async handle(request: Request): Promise<Reply> {
    try {
      return await this.#dispatch(request);
    } catch (error) {
      return reply(this.#explain(error));
    }
  }

  async #dispatch(request: Request): Promise<Reply> {
    const [head = "", ...rest] = request.args;

    switch (head.toLowerCase()) {
      case "":
        return reply(drawEmbed(await this.core.latestDraw()));
      case "quaythu":
      case "quaythử":
        return this.#spin(request.channelId);
      case "thongke":
      case "thốngkê":
        return this.#stats(rest);
      case "huongdan":
      case "help":
        return reply(helpEmbed(this.prefix, this.goldPrefix));
      default:
        return this.#byDate(request.args);
    }
  }

  // --- draws ---

  async #byDate(args: string[]): Promise<Reply> {
    const raw = args.join(" ");
    const iso = parseDate(raw);
    if (iso === null) {
      return reply(
        notice(
          "Không đọc được ngày",
          `Mình không hiểu \`${raw}\`.\nDùng dạng \`${this.prefix} 03/09/2026\`.`,
        ),
      );
    }
    return reply(drawEmbed(await this.core.draw(iso)));
  }

  // --- statistics ---

  async #stats(args: string[]): Promise<Reply> {
    const [head = "", ...rest] = args;

    switch (head.toLowerCase()) {
      case "db":
        return this.#specialMonth(rest);
      case "ngay":
      case "ngày":
        return this.#dayReport(rest);
      case "logan":
        return this.#gan("lo", "🧊 Lô lâu chưa về");
      case "degan":
        return this.#gan("de", "🧊 Đề lâu chưa về");
      case "tanso":
      case "tầnsố":
        return this.#frequency(rest);
      case "lo":
      case "lô":
        return this.#profile(rest);
      case "kho":
        return this.#archive();
      default:
        return reply(
          notice(
            "Không rõ lệnh",
            "Thống kê nhận: `ngay`, `db`, `logan`, `degan`, `tanso`, `lo`, `kho`.",
          ),
        );
    }
  }

  async #specialMonth(args: string[]): Promise<Reply> {
    let month: string | undefined;
    if (args.length > 0) {
      const parsed = parseMonth(args.join(" "));
      if (parsed === null) {
        return reply(
          notice(
            "Không đọc được tháng",
            `Dùng dạng \`${this.prefix} thongke db 09/2026\`.`,
          ),
        );
      }
      month = parsed;
    }
    return reply(specialMonthEmbed(await this.core.specialMonth(month)));
  }

  async #dayReport(args: string[]): Promise<Reply> {
    let date: string | undefined;
    if (args.length > 0) {
      const parsed = parseDate(args.join(" "));
      if (parsed === null) {
        return reply(
          notice(
            "Không đọc được ngày",
            `Dùng dạng \`${this.prefix} thongke ngay 03/09/2026\`.`,
          ),
        );
      }
      date = parsed;
    }
    return reply(dayReportEmbed(await this.core.dayReport(date)));
  }

  async #gan(scope: "lo" | "de", title: string): Promise<Reply> {
    const [entries, archive] = await Promise.all([
      this.core.gan(scope, GAN_LIMIT),
      this.core.archive(),
    ]);
    const asOf = formatVN(archive.latest ?? now().toISODate() ?? "");
    return reply(ganEmbed(title, entries, archive.draws, asOf));
  }

  /**
   * Either argument may come first: a number is a window, a word is a
   * grouping. Insisting on an order would only make the command harder to
   * remember.
   */
  async #frequency(args: string[]): Promise<Reply> {
    let days = DEFAULT_WINDOW;
    let group: Grouping | undefined;

    for (const arg of args) {
      const asNumber = Number(arg);
      if (Number.isInteger(asNumber) && arg.trim() !== "") {
        if (asNumber < 1) {
          return reply(
            notice(
              "Không đọc được số ngày",
              `\`${arg}\` phải là số ngày dương.\nDùng dạng \`${this.prefix} tanso 90\`.`,
            ),
          );
        }
        days = asNumber;
        continue;
      }
      const parsed = parseGrouping(arg);
      if (parsed === null) {
        return reply(
          notice(
            "Không đọc được tham số",
            `Mình không hiểu \`${arg}\`.\nTham số là số ngày, hoặc kiểu gom: ` +
              "`dau`, `duoi`, `tong`, `cham`.",
          ),
        );
      }
      group = parsed;
    }

    if (group === undefined) {
      return reply(
        notice(
          "Cần chọn kiểu gom",
          "Bản này mới hiển thị dạng gom nhóm. Thêm `dau`, `duoi`, `tong` hoặc `cham`.",
        ),
      );
    }
    const [grouped, archive] = await Promise.all([
      this.core.groupedFrequency(group, days),
      this.core.archive(),
    ]);
    return reply(groupedFrequencyEmbed(grouped, archive.draws));
  }

  /** A number profile. Core rejects anything that is not two digits, so the
   * check here is only to give a better message than core's. */
  async #profile(args: string[]): Promise<Reply> {
    const lo = (args[0] ?? "").trim();
    if (!/^\d{2}$/.test(lo)) {
      return reply(
        notice(
          "Cần một số hai chữ số",
          `Dùng dạng \`${this.prefix} thongke lo 27\`.`,
        ),
      );
    }
    return reply(profileEmbed(await this.core.profile(lo)));
  }

  // --- gold ---

  async gold(): Promise<Reply> {
    try {
      return reply(goldEmbed(await this.core.goldBoard()));
    } catch (error) {
      return reply(this.#explain(error));
    }
  }

  /**
   * The price chart. Core draws the PNG, so this only has to attach it.
   *
   * "chưa đủ dữ liệu" is its own answer rather than an error: the request was
   * fine, the source simply has too little history for that code.
   */
  async goldChart(args: string[]): Promise<Reply> {
    const { code, days } = parseChartArgs(args);
    try {
      const png = await this.core.goldChart(code, days);
      return {
        embed: {
          title: `📈 ${code} · ${days} ngày`,
          color: 0xb7791f,
          image: { url: "attachment://chart.png" },
        },
        files: [{ name: "chart.png", data: png }],
      };
    } catch (error) {
      if (error instanceof CoreError && error.code === "too_short") {
        return reply(
          notice(
            "Chưa đủ dữ liệu",
            `Nguồn chưa có đủ lịch sử của \`${code}\` để vẽ biểu đồ.`,
          ),
        );
      }
      return reply(this.#explain(error));
    }
  }

  async help(): Promise<Reply> {
    return reply(helpEmbed(this.prefix, this.goldPrefix));
  }

  async #archive(): Promise<Reply> {
    const archive = await this.core.archive();
    const span =
      archive.earliest && archive.latest
        ? `${formatVN(archive.earliest)} → ${formatVN(archive.latest)}`
        : "chưa có kỳ nào";
    return reply({
      title: "🗄️ Kho dữ liệu",
      description:
        `**${archive.draws.toLocaleString("vi-VN")}** kỳ · ${span}\n` +
        `${archive.absences} ngày không quay · ${archive.channels} kênh đăng ký`,
      color: 0x6b46c1,
    });
  }

  // --- spins ---

  async #spin(channelId: string): Promise<Reply> {
    const until = this.#spins.get(channelId);
    const at = Date.now();
    if (until !== undefined && at < until) {
      return reply(
        notice(
          "Đang quay",
          `Kênh này đang có một lượt quay. Thử lại sau ${Math.ceil((until - at) / 1000)} giây nhé.`,
        ),
      );
    }

    const spin = await this.core.spin();
    // One extra pace of slack, so the next spin cannot start into the tail of
    // this one's last edit.
    this.#spins.set(channelId, at + (spin.order.length + 1) * SPIN_PACE_MS);
    for (const [id, expiry] of this.#spins) {
      if (id !== channelId && expiry <= at) this.#spins.delete(id);
    }

    const frames = spinFrames(spin);
    const [first, ...rest] = frames;
    return { embed: first!, frames: rest, paceMs: SPIN_PACE_MS };
  }

  // --- subscriptions ---

  async subscribe(request: Request, on: boolean): Promise<Reply> {
    try {
      if (on) {
        const created = await this.core.subscribe(request.channelId, request.guildId);
        return reply(
          notice(
            created ? "Đã bật thông báo" : "Vốn đã bật",
            "Kênh này sẽ nhận kết quả XSMB tự động mỗi ngày, " +
              "ngay khi có đủ kết quả (thường sau 18h35 vài phút).",
          ),
        );
      }
      const removed = await this.core.unsubscribe(request.channelId);
      return reply(
        notice(
          removed ? "Đã tắt thông báo" : "Vốn đã tắt",
          "Kho vẫn tự cập nhật hằng ngày, chỉ là kênh này không nhận bài đăng nữa.",
        ),
      );
    } catch (error) {
      return reply(this.#explain(error));
    }
  }

  /**
   * Turns a failure into something a person can act on.
   *
   * The distinction between `not_yet` and `no_draw` is the reason the contract
   * keeps them apart: one means come back later, the other means never.
   */
  #explain(error: unknown): ReturnType<typeof notice> {
    if (!(error instanceof CoreError)) {
      return notice("Lỗi", "Có gì đó không ổn. Thử lại sau nhé.", true);
    }
    if (error.notYet) {
      return notice(
        "Chưa có kết quả",
        "Kỳ này chưa quay xong. Kết quả thường có sau 18h35 vài phút.",
      );
    }
    if (error.noDraw) {
      return notice("Không có kết quả", "Ngày này không có kỳ quay nào.");
    }
    if (error.notConfigured) {
      return notice("Chưa bật", "Tính năng này chưa được cấu hình.", true);
    }
    if (error.code === "out_of_range") {
      return notice(
        "Ngoài phạm vi",
        "Kho chỉ có từ 01/10/2005 tới hôm nay.",
      );
    }
    if (error.code === "bad_request") {
      return notice("Không hợp lệ", error.message);
    }
    return notice("Lỗi", "Không lấy được dữ liệu. Thử lại sau nhé.", true);
  }
}

const GROUPINGS: Record<string, Grouping> = {
  dau: "dau",
  đầu: "dau",
  duoi: "duoi",
  đuôi: "duoi",
  tong: "tong",
  tổng: "tong",
  cham: "cham",
  chạm: "cham",
};

export function parseGrouping(input: string): Grouping | null {
  return GROUPINGS[input.trim().toLowerCase()] ?? null;
}

/** Reads `SJC 90`, `90 SJC`, or either alone. Order-free for the same reason
 * tanso is: a command nobody can remember the order of is a command nobody
 * uses. */
export function parseChartArgs(args: string[]): { code: string; days: number } {
  let code = "SJC";
  let days = 30;
  for (const arg of args) {
    const asNumber = Number(arg);
    if (Number.isInteger(asNumber) && arg.trim() !== "" && asNumber >= 2) {
      // Not clamped here. The limit belongs to core, which knows what the
      // source keeps; a copy of it on this side is a copy that can drift, and
      // silently shrinking the request is the behaviour being fixed.
      days = asNumber;
      continue;
    }
    if (arg.trim() !== "") code = arg.trim().toUpperCase();
  }
  return { code, days };
}
