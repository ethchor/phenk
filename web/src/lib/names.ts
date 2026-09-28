/*
 * Inbox names.
 *
 * These mirror the server's rules so the name field can say what is wrong as
 * someone types (Entering data: "dynamically validate field values"), but the
 * server stays the authority: it also refuses reserved names, which this file
 * deliberately does not list.
 */

const NAME = /^[a-z0-9][a-z0-9._-]{2,63}$/;

export interface ParsedName {
  /** The local part as the server will use it: lower-cased, with any +tag removed. */
  local: string;
  /** A domain, when a full address was typed or pasted. */
  domain: string | null;
}

/** Accepts a bare name or a full address, the way people actually paste them. */
export function parseName(input: string): ParsedName {
  let value = input.trim().toLowerCase();
  let domain: string | null = null;

  const at = value.lastIndexOf("@");
  if (at >= 0) {
    domain = value.slice(at + 1) || null;
    value = value.slice(0, at);
  }
  const plus = value.indexOf("+");
  if (plus >= 0) value = value.slice(0, plus);
  return { local: value, domain };
}

/** Why a name would be refused, in words someone can act on — or null. */
export function nameProblem(local: string): string | null {
  if (local.length === 0) return null;
  if (local.length < 3) return "Use at least 3 characters.";
  if (local.length > 64) return "Use 64 characters or fewer.";
  if (!/^[a-z0-9]/.test(local)) return "Start with a letter or a digit.";
  if (!NAME.test(local)) return "Use only letters, digits, dots, dashes and underscores.";
  return null;
}

const ADJECTIVES = [
  "amber",
  "brisk",
  "calm",
  "clever",
  "cosmic",
  "crisp",
  "daring",
  "dusky",
  "eager",
  "fancy",
  "gentle",
  "glad",
  "golden",
  "hazy",
  "humble",
  "jolly",
  "keen",
  "lively",
  "lucky",
  "mellow",
  "misty",
  "nimble",
  "noble",
  "plucky",
  "polite",
  "quiet",
  "rapid",
  "rosy",
  "rustic",
  "shiny",
  "silver",
  "snowy",
  "sunny",
  "swift",
  "tidy",
  "velvet",
  "vivid",
  "witty",
  "zesty",
  "breezy",
];

const NOUNS = [
  "otter",
  "falcon",
  "maple",
  "harbor",
  "comet",
  "willow",
  "badger",
  "lantern",
  "meadow",
  "pebble",
  "heron",
  "canyon",
  "ember",
  "fjord",
  "garnet",
  "hazel",
  "island",
  "juniper",
  "kestrel",
  "lagoon",
  "marmot",
  "nebula",
  "orchid",
  "puffin",
  "quartz",
  "raven",
  "sparrow",
  "thistle",
  "tundra",
  "walrus",
  "yarrow",
  "zephyr",
  "acorn",
  "beacon",
  "cobalt",
  "dolphin",
  "fennel",
  "glacier",
  "lynx",
  "mesa",
];

/**
 * A readable random name, like YOPmail's random button: easy to read aloud and
 * to type into a form, and unlikely to collide. The dash means it can never be
 * mistaken for a generated private address, which the server refuses as a
 * public name.
 */
export function randomName(): string {
  const pick = <T>(list: readonly T[]): T => list[randomIndex(list.length)] as T;
  const digits = String(randomIndex(900) + 100);
  return `${pick(ADJECTIVES)}-${pick(NOUNS)}-${digits}`;
}

function randomIndex(n: number): number {
  const buffer = new Uint32Array(1);
  crypto.getRandomValues(buffer);
  return (buffer[0] ?? 0) % n;
}
