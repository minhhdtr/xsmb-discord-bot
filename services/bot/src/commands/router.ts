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
      // The typed forms of /thongbao and /thongke kho. They existed in the Go
      // bot and are documented, but were never wired here, so they fell
      // through to the date parser and answered "không đọc được ngày".
      // index.ts already gated sub/unsub on Manage Channels - a permission
      // check on commands that did not run.
      case "sub":
        return this.subscribe(request, true);
      case "unsub":
        return this.subscribe(request, false);
      case "status":
        return this.#archive();
      default:
        // The statistics also answer at the top level: the Go bot took
        // `!xsmb logan` and `!xsmb db 08/2026`, and the README still documents
        // them that way. Requiring `thongke` in the middle was a regression
        // introduced by the port, invisible because every test spelled the
        // long form out.
        if (STATS.has(head.toLowerCase())) {
          return this.#stats(request.args);
        }
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

  /**
   * Gold codes for the autocomplete on /bieudo.
   *
   * Taken from the board core already has cached, so the list is whatever the
   * source is actually quoting rather than a copy that goes out of date. When
   * core cannot answer - the feature is off, the source is down - a short
   * built-in list still lets someone type the common ones instead of being
   * left with an empty dropdown.
   */
  async goldCodes(prefix: string): Promise<{ name: string; value: string }[]> {
    let codes: { name: string; value: string }[];
    try {
      const board = await this.core.goldBoard();
      codes = board.quotes.map((q) => ({ name: q.name, value: q.code }));
    } catch (error) {
      this.log("autocomplete fell back to the built-in list", error);
      codes = FALLBACK_GOLD_CODES.map((code) => ({ name: code, value: code }));
    }

    const wanted = prefix.trim().toLowerCase();
    const matches = wanted
      ? codes.filter(
          (c) =>
            c.value.toLowerCase().includes(wanted) ||
            c.name.toLowerCase().includes(wanted),
        )
      : codes;
    // Discord refuses more than 25.
    return matches.slice(0, 25);
  }

  private log(message: string, error: unknown): void {
    console.warn(message, error);
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
    if (error.code === "upstream") {
      // Naming the source matters. "Không lấy được dữ liệu" is true of every
      // failure and therefore tells nobody anything - not the person reading
      // it, and not whoever they report it to.
      return notice(
        "Nguồn không trả lời",
        "Trang nguồn đang lỗi hoặc chậm. Thử lại sau vài phút nhé.",
        true,
      );
    }
    if (error.status === 0) {
      return notice(
        "Không gọi được core",
        "Bot không kết nối được tới core. Kiểm tra `docker compose ps`.",
        true,
      );
    }
    return notice(
      "Lỗi",
      `Có gì đó không ổn (${error.code}). Chi tiết nằm trong log của core.`,
      true,
    );
  }
}

/**
 * Enough to type with when core cannot say what the source is quoting.
 *
 * Real codes from the source, checked against a live response. An earlier
 * version of this list held SJC, PNJ, DOJI and XAU — none of which exist. They
 * were the obvious guesses, and being obvious is what made them wrong: the
 * source uses its own identifiers, and nobody had looked.
 */
const FALLBACK_GOLD_CODES = ["SJL1L10", "VNGSJC", "DOHNL", "PQHNVM", "XAUUSD"];

/**
 * What `/bieudo` charts when nothing is given: vàng miếng SJC, the one most
 * people mean.
 *
 * Not "SJC" — that is not a code the source knows, and defaulting to it made
 * the bare command fail while every test passed, because the fake core
 * answered for any code at all.
 */
const DEFAULT_GOLD_CODE = "SJL1L10";

/** Statistics that answer with or without `thongke` in front of them. */
const STATS = new Set([
  "db", "ngay", "ngày", "logan", "degan",
  "tanso", "tầnsố", "lo", "lô", "kho",
]);

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
  let code = DEFAULT_GOLD_CODE;
  let days = 30;

  for (const arg of args) {
    const asNumber = Number(arg);
    // Anything numeric is a window, including a number too small to be one.
    // Requiring >= 2 here made `!gold chart 1` mean "the gold code 1", and
    // `!gold chart SJC 1` silently threw the code away - the argument that
    // could not be a window quietly became the one thing left.
    //
    // Not clamped either: the limit belongs to core, which knows what the
    // source keeps, and a copy on this side is a copy that can drift.
    if (arg.trim() !== "" && Number.isInteger(asNumber)) {
      days = asNumber;
      continue;
    }
    if (arg.trim() !== "") code = arg.trim().toUpperCase();
  }
  return { code, days };
}
