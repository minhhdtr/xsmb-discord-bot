import {
  Client,
  Events,
  GatewayIntentBits,
  MessageFlags,
  PermissionFlagsBits,
  type ChatInputCommandInteraction,
  type Message,
} from "discord.js";

import { Core } from "./core/client.js";
import { Router, type Request } from "./commands/router.js";
import { Announcer } from "./commands/announce.js";
import { fromInteraction } from "./commands/interaction.js";
import { slashCommands } from "./commands/registry.js";
import type { APIEmbed } from "discord.js";
import type { Reply } from "./render/embeds.js";

const token = requireEnv("DISCORD_TOKEN");
const coreUrl = process.env.CORE_URL ?? "http://core:8080";
const prefix = process.env.COMMAND_PREFIX ?? "!xsmb";
const goldPrefix = process.env.GOLD_PREFIX ?? "!gold";
const guildId = process.env.DISCORD_GUILD_ID ?? "";
const prefixCommands = process.env.PREFIX_COMMANDS !== "false";

const core = new Core({ baseUrl: coreUrl });
const router = new Router(core, prefix, goldPrefix);
const stopping = new AbortController();

const client = new Client({
  // Message Content is privileged. Asking for it when the application has not
  // been granted it makes the gateway refuse the connection outright, so it is
  // only requested when prefix commands are actually wanted.
  intents: prefixCommands
    ? [
        GatewayIntentBits.Guilds,
        GatewayIntentBits.GuildMessages,
        GatewayIntentBits.DirectMessages,
        GatewayIntentBits.MessageContent,
      ]
    : [GatewayIntentBits.Guilds],
});

client.once(Events.ClientReady, async (ready) => {
  console.log(`connected as ${ready.user.tag}, core at ${coreUrl}`);

  // With a guild id the commands appear at once, which is what you want while
  // developing. Without one they are global and can take an hour to spread.
  const scope = guildId ? `guild ${guildId}` : "global";
  try {
    if (guildId) {
      await ready.application.commands.set(slashCommands(), guildId);
    } else {
      await ready.application.commands.set(slashCommands());
    }
    console.log(`slash commands registered (${scope})`);
  } catch (error) {
    console.error(`cannot register slash commands (${scope}):`, error);
  }
});

client.on(Events.InteractionCreate, async (interaction) => {
  if (interaction.isAutocomplete()) {
    // Discord gives three seconds and accepts no second attempt, so a failure
    // here means an empty dropdown rather than an error message. Answering
    // with something is always better than answering with nothing.
    try {
      const focused = interaction.options.getFocused(true);
      const choices =
        focused.name === "ma" ? await router.goldCodes(focused.value) : [];
      await interaction.respond(choices);
    } catch (error) {
      console.warn("autocomplete failed:", error);
    }
    return;
  }
  if (!interaction.isChatInputCommand()) return;
  try {
    await handleInteraction(interaction);
  } catch (error) {
    // Anything that escapes handleInteraction would otherwise be an unhandled
    // rejection and, to the person who ran the command, silence: Discord shows
    // "The application did not respond" and nothing explains why. Saying so is
    // worse than a good answer and far better than nothing.
    console.error(`/${interaction.commandName} failed:`, error);
    await say(interaction, "Lệnh này lỗi rồi. Mình đã ghi log, thử lại sau nhé.");
  }
});

if (prefixCommands) {
  client.on(Events.MessageCreate, async (message) => {
    if (message.author.bot) return;
    // The gold prefix is checked first: if one prefix is a prefix of the
    // other, whichever is tested first wins, and that should be a decision
    // rather than an accident of ordering.
    // A boundary is required, or `!xsmbfoo` counts as `!xsmb` and the bot
    // answers a message that was never addressed to it.
    if (addressedTo(message.content, goldPrefix)) {
      await handleMessage(message, goldPrefix, true);
    } else if (addressedTo(message.content, prefix)) {
      await handleMessage(message, prefix, false);
    }
  });
}

async function handleInteraction(interaction: ChatInputCommandInteraction): Promise<void> {
  const { args, ephemeral } = fromInteraction(interaction.commandName, interaction.options);
  const request: Request = {
    args,
    channelId: interaction.channelId,
    ...(interaction.guildId ? { guildId: interaction.guildId } : {}),
  };

  // Deferred first. A command that has to crawl a missing day takes longer
  // than the three seconds Discord allows for a first response.
  await interaction.deferReply(ephemeral ? { flags: MessageFlags.Ephemeral } : {});

  if (interaction.commandName === "thongbao" && !mayManageChannel(interaction)) {
    await say(interaction, refusal);
    return;
  }

  const reply = await route(request, interaction.commandName, interaction.options.getString("trangthai"));
  await interaction.editReply({
    embeds: [reply.embed],
    ...(reply.files
      ? { files: reply.files.map((f) => ({ attachment: f.data, name: f.name })) }
      : {}),
  });

  // Interaction edits go to a webhook route, a different rate limit bucket
  // from editing messages in the channel, so two spins in one channel do not
  // queue behind each other. The token lasts fifteen minutes, far longer than
  // any frame sequence.
  await playFrames(reply, async (frame) => {
    await interaction.editReply({ embeds: [frame] });
  });
}

async function handleMessage(message: Message, used: string, gold: boolean): Promise<void> {
  const args = message.content.slice(used.length).trim().split(/\s+/).filter(Boolean);
  const request: Request = {
    args,
    channelId: message.channelId,
    ...(message.guildId ? { guildId: message.guildId } : {}),
  };

  // A group DM has no send(); nothing else the bot can reach lacks it.
  if (!message.channel.isSendable()) return;

  if (!gold && isSubscriptionCommand(args) && !mayManageChannel(message)) {
    await message.reply(refusal);
    return;
  }

  const reply = await routePrefix(request, gold);
  const sent = await message.channel.send({
    embeds: [reply.embed],
    ...(reply.files
      ? { files: reply.files.map((f) => ({ attachment: f.data, name: f.name })) }
      : {}),
  });
  await playFrames(reply, async (frame) => {
    await sent.edit({ embeds: [frame] });
  });
}

async function route(
  request: Request,
  command: string,
  state: string | null,
): Promise<Reply> {
  switch (command) {
    case "thongbao":
      return router.subscribe(request, state !== "tắt");
    case "gold":
      return router.gold();
    case "bieudo":
      return router.goldChart(request.args);
    case "huongdan":
      return router.help();
    default:
      return router.handle(request);
  }
}

/**
 * Edits a message through the rest of a reply's frames.
 *
 * A failed edit stops the sequence. Carrying on would mean waiting out the
 * remaining frames to show a board that never updates, and the usual cause —
 * the message being deleted — will only fail again.
 */
async function playFrames(
  reply: Reply,
  edit: (frame: APIEmbed) => Promise<void>,
): Promise<void> {
  const frames = reply.frames ?? [];
  if (frames.length === 0) return;
  const pace = reply.paceMs ?? 1_000;

  for (const frame of frames) {
    await sleep(pace);
    try {
      await edit(frame);
    } catch (error) {
      console.warn("stopped a frame sequence:", error);
      return;
    }
  }
}

const refusal =
  "Lệnh này cần quyền **Quản lý kênh** — thông báo hằng ngày là cài đặt của cả kênh, không phải của riêng ai.";

/** Whether the caller may change this channel's subscription.
 *
 * Checked at runtime as well as declared on the command, because
 * defaultMemberPermissions can be overridden per guild and does not apply to
 * the typed form at all. A DM has no channel to manage, so it fails here too.
 */
function mayManageChannel(source: ChatInputCommandInteraction | Message): boolean {
  if (!source.inGuild()) return false;
  // An interaction carries the resolved permissions; a message carries the
  // member, whose permissions are resolved against the channel it arrived in.
  const permissions =
    "memberPermissions" in source ? source.memberPermissions : source.member?.permissions;
  return permissions?.has(PermissionFlagsBits.ManageChannels) ?? false;
}

/** The typed forms that change a subscription. */
function isSubscriptionCommand(args: string[]): boolean {
  const head = (args[0] ?? "").toLowerCase();
  return head === "sub" || head === "unsub" || head === "thongbao";
}

/** True when the message is this command and not merely starts with its
 * letters. */
function addressedTo(content: string, prefix: string): boolean {
  const text = content.trimEnd();
  return text === prefix || content.startsWith(prefix + " ") || content.startsWith(prefix + "\n");
}

/** Replies however the interaction still allows: a fresh reply if nothing has
 * been sent, an edit if it was already deferred. */
async function say(interaction: ChatInputCommandInteraction, text: string): Promise<void> {
  const body = { content: text, embeds: [] };
  try {
    if (interaction.deferred || interaction.replied) {
      await interaction.editReply(body);
    } else {
      await interaction.reply({ ...body, flags: MessageFlags.Ephemeral });
    }
  } catch (error) {
    console.error("cannot even report the failure:", error);
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function requireEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    console.error(`${name} is not set`);
    process.exit(1);
  }
  return value;
}

// The announcer runs beside the gateway. It only reads from core and posts;
// filling the archive is core's job and happens whether this is up or not.
const announcer = new Announcer(core, async (channelId, embed) => {
  const channel = await client.channels.fetch(channelId);
  if (channel?.isSendable()) await channel.send({ embeds: [embed] });
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => {
    stopping.abort();
    void client.destroy();
  });
}

await client.login(token);
void announcer.run(stopping.signal);

/**
 * Prefix commands carry the gold ones under a prefix of their own, so the
 * split happens here rather than inside the router. Slash commands do not
 * need it: there, `/gold` and `/bieudo` are separate commands already.
 */
async function routePrefix(request: Request, gold: boolean): Promise<Reply> {
  if (!gold) return router.handle(request);

  const [head = "", ...rest] = request.args;
  switch (head.toLowerCase()) {
    case "chart":
    case "bieudo":
      return router.goldChart(rest);
    case "help":
    case "huongdan":
      return router.help();
    default:
      return router.gold();
  }
}
