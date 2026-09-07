/**
 * Number formatting, matched to what the Go bot produced so the two read the
 * same during the port.
 *
 * `Intl.NumberFormat("vi-VN")` gives the dot separator Vietnamese uses, which
 * is the whole reason this is short: the only thing worth writing by hand is
 * the rounding, and the reason for that is upstream noise.
 */

const vn = (places: number) =>
  new Intl.NumberFormat("vi-VN", {
    minimumFractionDigits: places,
    maximumFractionDigits: places,
  });

/**
 * Rounds to `places` before anything is shown.
 *
 * Prices from the source carry float noise — a movement of exactly 7.6 arrives
 * as 7.599999999999454. Formatting that without rounding first turns a flat
 * day into a suspiciously precise one.
 */
export function round(value: number, places: number): number {
  const factor = 10 ** places;
  return Math.round(value * factor) / factor;
}

export function decimal(value: number, places = 0): string {
  return vn(places).format(round(value, places));
}

/** Đồng, always whole: the source never quotes a fraction of one. */
export function dong(amount: number): string {
  return decimal(amount, 0) + "₫";
}

/** A movement with an arrow. Zero gets its own glyph rather than a signed
 * zero, which reads as an error. */
export function changeLabel(delta: number, places = 0): string {
  const rounded = round(delta, places);
  if (rounded > 0) return "↑ " + decimal(rounded, places);
  if (rounded < 0) return "↓ " + decimal(-rounded, places);
  return "→ 0";
}

export function changeDong(delta: number): string {
  const rounded = round(delta, 0);
  if (rounded > 0) return "↑ " + dong(rounded);
  if (rounded < 0) return "↓ " + dong(-rounded);
  return "→ 0₫";
}
