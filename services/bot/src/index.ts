import {
  Client,
  Events,
  GatewayIntentBits,
  MessageFlags,
  type ChatInputCommandInteraction,
  type Message,
} from "discord.js";

import { Core } from "./core/client.js";
import { Router, type Request } from "./commands/router.js";
import { Announcer } from "./commands/announce.js";
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
  if (!interaction.isChatInputCommand()) return;
  await handleInteraction(interaction);
});

if (prefixCommands) {
  client.on(Events.MessageCreate, async (message) => {
    if (message.author.bot) return;
    // The gold prefix is checked first: if one prefix is a prefix of the
    // other, whichever is tested first wins, and that should be a decision
    // rather than an accident of ordering.
    if (message.content.startsWith(goldPrefix)) {
      await handleMessage(message, goldPrefix, true);
    } else if (message.content.startsWith(prefix)) {
      await handleMessage(message, prefix, false);
    }
  });
}

async function handleInteraction(interaction: ChatInputCommandInteraction): Promise<void> {
  const { args, ephemeral } = fromInteraction(interaction);
  const request: Request = {
    args,
    channelId: interaction.channelId,
    ...(interaction.guildId ? { guildId: interaction.guildId } : {}),
  };

  // Deferred first. A command that has to crawl a missing day takes longer
  // than the three seconds Discord allows for a first response.
  await interaction.deferReply(ephemeral ? { flags: MessageFlags.Ephemeral } : {});

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

/** Flattens slash options into the same word list a prefix command produces,
 * so there is one handler per command rather than two. */
function fromInteraction(interaction: ChatInputCommandInteraction): {
  args: string[];
  ephemeral: boolean;
} {
  const name = interaction.commandName;

  if (name === "xsmb") {
    const day = interaction.options.getString("ngay");
    return { args: day ? day.split(/\s+/) : [], ephemeral: false };
  }
  if (name === "quaythu") {
    return { args: ["quaythu"], ephemeral: false };
  }
  if (name === "thongbao") {
    return { args: [], ephemeral: true };
  }
  if (name === "gold" || name === "huongdan") {
    return { args: [], ephemeral: name === "huongdan" };
  }
  if (name === "bieudo") {
    const args: string[] = [];
    const code = interaction.options.getString("ma");
    if (code) args.push(code);
    const days = interaction.options.getInteger("ngay");
    if (days !== null) args.push(String(days));
    return { args, ephemeral: false };
  }
  if (name === "thongke") {
    const sub = interaction.options.getSubcommand();
    const args = ["thongke", sub];
    for (const option of ["ngay", "thang", "kieu"]) {
      const value = interaction.options.getString(option);
      if (value) args.push(...value.split(/\s+/));
    }
    const days = interaction.options.getInteger("ngay");
    if (days !== null) args.push(String(days));
    return { args, ephemeral: false };
  }
  return { args: [name], ephemeral: false };
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
  if (head.toLowerCase() === "chart" || head.toLowerCase() === "bieudo") {
    return router.goldChart(rest);
  }
  return router.gold();
}
