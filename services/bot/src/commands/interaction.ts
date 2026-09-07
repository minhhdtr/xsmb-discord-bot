import type { CommandInteractionOptionResolver } from "discord.js";

/**
 * The flattened form of a slash command: the same word list a typed command
 * produces, so there is one handler per command rather than two.
 */
export interface Parsed {
  args: string[];
  /** Only the person who asked sees the reply. */
  ephemeral: boolean;
}

/** The parts of the resolver used here. Narrowing it this way lets a test
 * build a real `CommandInteractionOptionResolver` from a real option payload
 * rather than a mock that agrees with whatever the test author assumed. */
type Options = Pick<
  CommandInteractionOptionResolver,
  "getSubcommand" | "getString" | "getInteger"
>;

/**
 * Reads a slash command into arguments.
 *
 * Every option is read with the getter matching the type it was **registered**
 * with. That sounds obvious and is the whole point: an earlier version looped
 * over a list of option names calling `getString` on each, which threw
 *
 *     Option "ngay" is of type: 4; expected 3
 *
 * for `/thongke tanso`, because `ngay` is registered as an Integer there. The
 * throw happened before `deferReply`, so Discord showed "The application did
 * not respond" and the bot logged an unhandled rejection. The same loop also
 * never looked for `so`, so `/thongke lo` silently lost its argument.
 *
 * One list of names cannot serve subcommands whose options differ in name and
 * in type. Each is read on its own here, and the tests beside this file cover
 * every subcommand that takes one.
 */
export function fromInteraction(commandName: string, options: Options): Parsed {
  switch (commandName) {
    case "xsmb": {
      const day = options.getString("ngay");
      return { args: day ? day.split(/\s+/) : [], ephemeral: false };
    }

    case "quaythu":
      return { args: ["quaythu"], ephemeral: false };

    case "gold":
      return { args: [], ephemeral: false };

    case "huongdan":
      return { args: [], ephemeral: true };

    case "thongbao":
      return { args: [], ephemeral: true };

    case "bieudo": {
      const args: string[] = [];
      const code = options.getString("ma");
      if (code) args.push(code);
      const days = options.getInteger("ngay");
      if (days !== null) args.push(String(days));
      return { args, ephemeral: false };
    }

    case "thongke":
      return { args: thongke(options), ephemeral: false };

    default:
      return { args: [commandName], ephemeral: false };
  }
}

function thongke(options: Options): string[] {
  const sub = options.getSubcommand();
  const args = ["thongke", sub];

  switch (sub) {
    case "ngay": {
      // Registered as a String: people type 03/09/2026, which is not a number.
      const day = options.getString("ngay");
      if (day) args.push(...day.split(/\s+/));
      break;
    }
    case "db": {
      const month = options.getString("thang");
      if (month) args.push(...month.split(/\s+/));
      break;
    }
    case "tanso": {
      const kind = options.getString("kieu");
      if (kind) args.push(kind);
      // Registered as an Integer here, unlike the `ngay` above. Two options
      // with one name and two types is exactly what broke the old loop.
      const days = options.getInteger("ngay");
      if (days !== null) args.push(String(days));
      break;
    }
    case "lo": {
      const lo = options.getString("so");
      if (lo) args.push(lo);
      break;
    }
    // logan, degan and kho take no options.
  }
  return args;
}
