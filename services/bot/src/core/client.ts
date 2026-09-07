import type { components, paths } from "./schema.js";

/**
 * Every shape here is pulled out of the generated file rather than written by
 * hand. That is the whole reason the generator is in the build: once the bot
 * left Go, a renamed field stopped being a compile error. Naming these through
 * `components["schemas"][...]` puts that safety back — rename a field in
 * `contracts/openapi.yaml`, regenerate, and `tsc` fails here instead of the
 * bot failing at 18:45 in front of everyone.
 */
export type Draw = components["schemas"]["Draw"];
export type DayReport = components["schemas"]["DayReport"];
export type Gan = components["schemas"]["Gan"];
export type FrequencyList = components["schemas"]["FrequencyList"];
export type GroupedFrequency = components["schemas"]["GroupedFrequency"];
export type Profile = components["schemas"]["Profile"];
export type SpecialMonth = components["schemas"]["SpecialMonth"];
export type Archive = components["schemas"]["Archive"];
export type Spin = components["schemas"]["Spin"];
export type GoldBoard = components["schemas"]["GoldBoard"];
export type GoldSeries = components["schemas"]["GoldSeries"];
export type Subscription = components["schemas"]["Subscription"];
export type ApiError = components["schemas"]["Error"];

/** The error codes core sends. Taken from the schema so a new one added to the
 * contract shows up here as a type error in every exhaustive switch. */
export type ErrorCode = ApiError["code"];

/** Grouping values accepted by /v1/stats/frequency. */
export type Grouping = NonNullable<
  paths["/v1/stats/frequency"]["get"]["parameters"]["query"]
>["group"];

/**
 * CoreError carries the code, never just a message.
 *
 * Callers switch on `code`. Two of core's 404s mean different things — one is
 * worth asking about again, one never will be — and a client that only sees
 * the status cannot tell them apart.
 */
export class CoreError extends Error {
  constructor(
    readonly code: ErrorCode | "unknown",
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "CoreError";
  }

  /** The result is not published yet. Worth asking again shortly. */
  get notYet(): boolean {
    return this.code === "not_yet";
  }

  /** The day is settled and empty. Asking again will never help. */
  get noDraw(): boolean {
    return this.code === "no_draw";
  }

  /** The feature is switched off in this deployment, which is not a failure. */
  get notConfigured(): boolean {
    return this.code === "not_configured";
  }
}

export interface CoreOptions {
  baseUrl: string;
  /** Long enough to cover a crawl of a day the archive is missing, which is
   * the slowest thing behind any route. */
  timeoutMs?: number;
}

export class Core {
  readonly #base: string;
  readonly #timeout: number;

  constructor(options: CoreOptions) {
    this.#base = options.baseUrl.replace(/\/+$/, "");
    this.#timeout = options.timeoutMs ?? 20_000;
  }

  async #request(path: string, init?: RequestInit): Promise<Response> {
    const signal = AbortSignal.timeout(this.#timeout);
    let response: Response;
    try {
      response = await fetch(this.#base + path, { ...init, signal });
    } catch (cause) {
      throw new CoreError("unknown", 0, `core unreachable: ${String(cause)}`);
    }
    if (!response.ok) {
      throw await toCoreError(response);
    }
    return response;
  }

  async #json<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await this.#request(path, init);
    return (await response.json()) as T;
  }

  // --- draws ---

  latestDraw(): Promise<Draw> {
    return this.#json<Draw>("/v1/draws/latest");
  }

  /** `date` is ISO. The Vietnamese forms people type are parsed before here. */
  draw(date: string): Promise<Draw> {
    return this.#json<Draw>(`/v1/draws/${encodeURIComponent(date)}`);
  }

  // --- statistics ---

  dayReport(date?: string): Promise<DayReport> {
    return this.#json<DayReport>("/v1/stats/day" + qs({ date }));
  }

  gan(scope: "lo" | "de", limit?: number): Promise<Gan[]> {
    return this.#json<Gan[]>("/v1/stats/gan" + qs({ scope, limit }));
  }

  frequency(days?: number): Promise<FrequencyList> {
    return this.#json<FrequencyList>("/v1/stats/frequency" + qs({ days }));
  }

  groupedFrequency(group: Grouping, days?: number): Promise<GroupedFrequency> {
    return this.#json<GroupedFrequency>(
      "/v1/stats/frequency" + qs({ days, group }),
    );
  }

  profile(lo: string): Promise<Profile> {
    return this.#json<Profile>(`/v1/stats/number/${encodeURIComponent(lo)}`);
  }

  specialMonth(month?: string): Promise<SpecialMonth> {
    return this.#json<SpecialMonth>("/v1/stats/special-month" + qs({ month }));
  }

  archive(): Promise<Archive> {
    return this.#json<Archive>("/v1/archive");
  }

  // --- spins ---

  spin(): Promise<Spin> {
    return this.#json<Spin>("/v1/spins", { method: "POST" });
  }

  // --- gold ---

  goldBoard(): Promise<GoldBoard> {
    return this.#json<GoldBoard>("/v1/gold");
  }

  goldHistory(code: string, days?: number): Promise<GoldSeries> {
    return this.#json<GoldSeries>(
      `/v1/gold/${encodeURIComponent(code)}/history` + qs({ days }),
    );
  }

  /** The one endpoint that answers with bytes rather than JSON. */
  async goldChart(code: string, days?: number): Promise<Buffer> {
    const response = await this.#request(
      `/v1/gold/${encodeURIComponent(code)}/chart.png` + qs({ days }),
    );
    return Buffer.from(await response.arrayBuffer());
  }

  // --- subscriptions ---

  subscriptions(): Promise<Subscription[]> {
    return this.#json<Subscription[]>("/v1/subscriptions");
  }

  async subscribe(channelId: string, guildId?: string): Promise<boolean> {
    const body = await this.#json<{ created: boolean }>(
      `/v1/subscriptions/${encodeURIComponent(channelId)}`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ guild_id: guildId ?? "" }),
      },
    );
    return body.created;
  }

  async unsubscribe(channelId: string): Promise<boolean> {
    const body = await this.#json<{ removed: boolean }>(
      `/v1/subscriptions/${encodeURIComponent(channelId)}`,
      { method: "DELETE" },
    );
    return body.removed;
  }

  /**
   * Reports whether this caller may post.
   *
   * Losing the race is an ordinary outcome, not an error: a restart at 18:50
   * must stay quiet rather than post the day a second time. So a conflict
   * becomes `false` here, and anything else still throws.
   */
  async claimAnnouncement(date: string, channelId: string): Promise<boolean> {
    try {
      await this.#request(
        `/v1/announcements/${encodeURIComponent(date)}/${encodeURIComponent(channelId)}`,
        { method: "POST" },
      );
      return true;
    } catch (error) {
      if (error instanceof CoreError && error.code === "already_claimed") {
        return false;
      }
      throw error;
    }
  }

  /** Ends the lease. Until this is called the claim expires on its own, which
   * is what lets a crash between claiming and sending be recovered from. */
  async markAnnounced(date: string, channelId: string): Promise<void> {
    await this.#request(
      `/v1/announcements/${encodeURIComponent(date)}/${encodeURIComponent(channelId)}`,
      { method: "PUT" },
    );
  }

  async releaseAnnouncement(date: string, channelId: string): Promise<void> {
    await this.#request(
      `/v1/announcements/${encodeURIComponent(date)}/${encodeURIComponent(channelId)}`,
      { method: "DELETE" },
    );
  }
}

async function toCoreError(response: Response): Promise<CoreError> {
  let code: ErrorCode | "unknown" = "unknown";
  let message = response.statusText;
  try {
    const body = (await response.json()) as Partial<ApiError>;
    if (body.code) code = body.code;
    if (body.message) message = body.message;
  } catch {
    // A body that is not the documented shape leaves the status as the only
    // information, which the fields above already carry.
  }
  return new CoreError(code, response.status, message);
}

/** qs drops undefined rather than sending `days=undefined`. */
function qs(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined) search.set(key, String(value));
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}
