import { DateTime } from "luxon";

/** The draw runs on Hanoi time, so every date in the bot is read in it. A
 * server running in UTC would otherwise disagree with core about what "today"
 * means for seven hours a day. */
export const ZONE = "Asia/Ho_Chi_Minh";

/**
 * Weekday names. A lookup table, not a rule — which is why it is duplicated
 * here rather than fetched.
 *
 * The distinction matters: 18:35, `LatestPublished`, and what counts as a
 * complete board are rules, they can change, and they live in core so there is
 * one copy. The Vietnamese word for Friday is not going to change.
 */
const WEEKDAYS = [
  "Thứ Hai",
  "Thứ Ba",
  "Thứ Tư",
  "Thứ Năm",
  "Thứ Sáu",
  "Thứ Bảy",
  "Chủ Nhật",
] as const;

export function now(): DateTime {
  return DateTime.now().setZone(ZONE);
}

/** `2026-09-04` → `Thứ Sáu`. */
export function weekdayVN(iso: string): string {
  const day = DateTime.fromISO(iso, { zone: ZONE });
  return day.isValid ? (WEEKDAYS[day.weekday - 1] ?? "") : "";
}

/** `2026-09-04` → `04/09/2026`, the form people read. */
export function formatVN(iso: string): string {
  const day = DateTime.fromISO(iso, { zone: ZONE });
  return day.isValid ? day.toFormat("dd/MM/yyyy") : iso;
}

/** `2026-09-04T18:45:57+07:00` → `18:45 04/09/2026`. */
export function formatStamp(iso: string | null | undefined): string {
  if (!iso) return "";
  const at = DateTime.fromISO(iso, { zone: ZONE });
  return at.isValid ? at.toFormat("HH:mm dd/MM/yyyy") : "";
}

/**
 * Reads the forms a person types and returns an ISO date for core.
 *
 * Core only accepts ISO, on purpose: a machine interface should not have to
 * guess whether `03/09` is March or September. Guessing is this side's job,
 * and the guesses are Vietnamese conventions — day first, two-digit years
 * assumed to be this century, a bare day meaning this month.
 */
export function parseDate(input: string, today = now()): string | null {
  const text = input.trim().replace(/\s+/g, "");
  if (text === "") return null;

  for (const word of ["hômnay", "homnay", "naỳ", "nay"]) {
    if (text.toLowerCase() === word) return today.toISODate();
  }
  if (["hômqua", "homqua", "qua"].includes(text.toLowerCase())) {
    return today.minus({ days: 1 }).toISODate();
  }

  // ISO first, before anything day-first touches it. `2026-09-03` splits into
  // three parts like `03-09-2026` does, and reading it day-first turns it into
  // day 2026 of month 9 in year 3 — which is invalid, so it would surface as
  // "không đọc được ngày" rather than as a wrong date. Still worth catching
  // here: core speaks ISO, so ISO is the one form guaranteed to reach this.
  if (/^\d{4}-\d{1,2}-\d{1,2}$/.test(text)) {
    const iso = DateTime.fromISO(text, { zone: ZONE });
    return iso.isValid ? iso.toISODate() : null;
  }

  const parts = text.split(/[/\-.]/).filter(Boolean);
  let day: number, month: number, year: number;

  if (parts.length === 1 && /^\d{1,2}$/.test(text)) {
    day = Number(text);
    month = today.month;
    year = today.year;
  } else if (parts.length === 2) {
    day = Number(parts[0]);
    month = Number(parts[1]);
    year = today.year;
  } else if (parts.length === 3) {
    day = Number(parts[0]);
    month = Number(parts[1]);
    year = Number(parts[2]);
    if (year < 100) year += 2000;
  } else {
    // Already ISO? Accept it rather than mangling it.
    const iso = DateTime.fromISO(text, { zone: ZONE });
    return iso.isValid ? iso.toISODate() : null;
  }

  if (!Number.isInteger(day) || !Number.isInteger(month) || !Number.isInteger(year)) {
    return null;
  }
  const parsed = DateTime.fromObject({ day, month, year }, { zone: ZONE });
  return parsed.isValid ? parsed.toISODate() : null;
}

/** Reads `09/2026`, `9/2026`, `2026-09`, or a bare `09` meaning this year. */
export function parseMonth(input: string, today = now()): string | null {
  const text = input.trim().replace(/\s+/g, "");
  if (text === "") return null;

  const iso = /^(\d{4})-(\d{1,2})$/.exec(text);
  if (iso) return monthISO(Number(iso[1]), Number(iso[2]));

  const slashed = /^(\d{1,2})[/\-.](\d{4})$/.exec(text);
  if (slashed) return monthISO(Number(slashed[2]), Number(slashed[1]));

  if (/^\d{1,2}$/.test(text)) return monthISO(today.year, Number(text));
  return null;
}

function monthISO(year: number, month: number): string | null {
  if (month < 1 || month > 12) return null;
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}`;
}

/** `2026-09` → `09/2026`, for a heading. */
export function formatMonth(iso: string): string {
  const [year, month] = iso.split("-");
  return year && month ? `${month}/${year}` : iso;
}
