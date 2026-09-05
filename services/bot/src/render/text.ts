/**
 * Width and padding for the monospace blocks the bot posts.
 *
 * `"Đặc biệt".length` is not the number of characters on screen. JavaScript
 * counts UTF-16 code units, and Vietnamese text can arrive decomposed — `ệ` as
 * `e` plus two combining marks — in which case the same word measures 8 or 11
 * depending on where the string came from. Padding by that number skews every
 * row of a table, and the result reads as a rendering glitch rather than an
 * encoding mistake, so nobody looks in the right place.
 *
 * Normalising to NFC first collapses the marks back into single code points.
 * Spreading the string then counts code points rather than code units, which
 * also keeps an emoji from counting as two.
 *
 * Most tables come from core already drawn — the column arithmetic lives in Go
 * where it is written once. These helpers are for the pieces the bot assembles
 * itself, and for asserting that what core sent still lines up.
 */

/** Characters wide, as a reader sees it. */
export function width(text: string): number {
  return [...text.normalize("NFC")].length;
}

/** Pads on the right to `columns`, never truncating. */
export function padEnd(text: string, columns: number): string {
  const normalized = text.normalize("NFC");
  const short = columns - width(normalized);
  return short > 0 ? normalized + " ".repeat(short) : normalized;
}

/** Pads on the left, for numbers in a column. */
export function padStart(text: string, columns: number): string {
  const normalized = text.normalize("NFC");
  const short = columns - width(normalized);
  return short > 0 ? " ".repeat(short) + normalized : normalized;
}

/** Wraps a block in a Discord code fence. Trailing blank lines are dropped so
 * the fence does not gain an empty row. */
export function fence(block: string): string {
  return "```\n" + block.replace(/\n+$/, "") + "\n```";
}
