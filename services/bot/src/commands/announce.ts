import type { APIEmbed } from "discord.js";
import { Core, CoreError } from "../core/client.js";
import { drawEmbed } from "../render/stats.js";
import { now, ZONE } from "../render/dates.js";
import { DateTime } from "luxon";

/** Where a finished post goes. Keeping this a function rather than a client
 * means the schedule can be tested without a gateway connection. */
export type Poster = (channelId: string, embed: APIEmbed) => Promise<void>;

export interface AnnouncerOptions {
  /** How long to keep asking before giving up until tomorrow. */
  windowMs?: number;
  /** Gap between attempts. */
  intervalMs?: number;
  /** Pause between sends, so a fan-out to many channels does not burst. */
  gapMs?: number;
}

/**
 * Posts the day's result to every subscribed channel, once.
 *
 * It does not crawl. Core's own ingest fills the archive on its own schedule
 * whether or not anybody is subscribed — that separation is what fixed the
 * 04/09 bug, where turning off the last subscription silently stopped the
 * archive from updating. This only has to notice.
 *
 * Asking rather than being pushed also keeps the dependency pointing one way:
 * the bot knows where core is, core knows nothing about the bot. A second
 * client can be added without core changing at all.
 */
export class Announcer {
  readonly #window: number;
  readonly #interval: number;
  readonly #gap: number;

  constructor(
    private readonly core: Core,
    private readonly post: Poster,
    options: AnnouncerOptions = {},
  ) {
    this.#window = options.windowMs ?? 45 * 60_000;
    this.#interval = options.intervalMs ?? 20_000;
    this.#gap = options.gapMs ?? 150;
  }

  /** Runs until `signal` aborts. */
  async run(signal: AbortSignal): Promise<void> {
    while (!signal.aborted) {
      const wait = msUntilNextRun();
      console.log(`announcer waiting ${Math.round(wait / 60_000)} minutes`);
      if (await sleep(wait, signal)) return;
      if (signal.aborted) return;

      await this.runFor(now().toISODate() ?? "", signal);
    }
  }

  /**
   * Chases one day's result and posts it.
   *
   * The claim is taken before the post and given back if the post fails, so a
   * restart at 18:50 does not put the same result out twice and a Discord
   * hiccup does not lose the day.
   */
  async runFor(date: string, signal?: AbortSignal): Promise<void> {
    const draw = await this.#await(date, signal);
    if (!draw) return;

    let subscriptions;
    try {
      subscriptions = await this.core.subscriptions();
    } catch (error) {
      console.error("cannot read subscriptions:", error);
      return;
    }
    if (subscriptions.length === 0) {
      console.log(`archived ${date}, no channels to announce to`);
      return;
    }

    const embed = drawEmbed(draw, true);
    for (const subscription of subscriptions) {
      if (signal?.aborted) return;
      await this.#postOnce(date, subscription.channel_id, embed);
      await sleep(this.#gap);
    }
  }

  async #postOnce(date: string, channelId: string, embed: APIEmbed): Promise<void> {
    let won: boolean;
    try {
      won = await this.core.claimAnnouncement(date, channelId);
    } catch (error) {
      console.error(`cannot claim ${date} for ${channelId}:`, error);
      return;
    }
    if (!won) return; // Someone already posted it. Staying quiet is the point.

    try {
      await this.post(channelId, embed);
    } catch (error) {
      console.error(`cannot post to ${channelId}:`, error);
      // Give the claim back so the next run tries again rather than the day
      // being lost to a transient failure.
      await this.core.releaseAnnouncement(date, channelId).catch(() => {});
      return;
    }

    // Sent. Ending the lease here rather than at claim time is what makes a
    // crash recoverable: a claim left unsent expires and can be taken over,
    // while this one never will be.
    await this.core.markAnnounced(date, channelId).catch((error: unknown) => {
      // The message is out. Failing to record that only risks a duplicate
      // after a crash inside the lease window, which is the lesser problem.
      console.error(`posted to ${channelId} but could not mark it sent:`, error);
    });
  }

  /** Asks core for the day until it has it, or the window closes. */
  async #await(date: string, signal?: AbortSignal) {
    const deadline = Date.now() + this.#window;

    while (Date.now() < deadline) {
      if (signal?.aborted) return null;
      try {
        return await this.core.draw(date);
      } catch (error) {
        if (worthRetrying(error)) {
          // Two different reasons to wait, both temporary. `not_yet` is the
          // ordinary one: the source publishes tier by tier, so the first
          // several minutes after the mark look like this. A timeout or a 5xx
          // is core being restarted or briefly unreachable — which used to
          // lose the whole day, because any error that was not `not_yet` ended
          // the attempt.
          if (await sleep(this.#interval, signal)) return null;
          continue;
        }
        console.error(`cannot get ${date}, giving up:`, error);
        return null;
      }
    }
    console.warn(`gave up on ${date} after ${Math.round(this.#window / 60_000)} minutes`);
    return null;
  }
}

/**
 * Whether an error is worth waiting through.
 *
 * The split is between "not there yet" and "will never be there". A day that
 * is settled and empty, or outside the archive, cannot be fixed by asking
 * again; a timeout, a 5xx, or an unreachable core all can. Getting this wrong
 * in the safe direction only costs requests inside a window that closes on its
 * own after forty-five minutes.
 */
export function worthRetrying(error: unknown): boolean {
  if (!(error instanceof CoreError)) {
    // fetch itself failed: DNS, connection refused, the socket dropped.
    return true;
  }
  if (error.notYet) return true;
  if (error.noDraw || error.code === "out_of_range" || error.code === "bad_request") {
    return false;
  }
  // 0 is a client-side failure; 5xx is core's problem and usually brief.
  return error.status === 0 || error.status >= 500;
}

/**
 * Milliseconds until the next 18:35 in Hanoi.
 *
 * Computed from the clock each time rather than by adding 24 hours, so a
 * restart at 18:40 waits until tomorrow instead of firing immediately, and a
 * process left running for weeks does not drift.
 */
export function msUntilNextRun(from: DateTime = now()): number {
  const at = from.setZone(ZONE);
  let next = at.set({ hour: 18, minute: 35, second: 0, millisecond: 0 });
  if (next <= at) next = next.plus({ days: 1 });
  return next.diff(at).toMillis();
}

/** Resolves true if the wait was cut short by the signal. */
function sleep(ms: number, signal?: AbortSignal): Promise<boolean> {
  return new Promise((resolve) => {
    if (signal?.aborted) return resolve(true);
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve(false);
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      resolve(true);
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}
