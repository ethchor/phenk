"use client";

import { useState, type FormEvent } from "react";

/*
 * The product, on the front page: type a name and go straight to its inbox in
 * the app. The rules mirror the server's so a mistake is caught while typing;
 * the server stays the authority.
 */

const NAME = /^[a-z0-9][a-z0-9._-]{2,63}$/;

function localPart(input: string): string {
  let value = input.trim().toLowerCase();
  const at = value.lastIndexOf("@");
  if (at >= 0) value = value.slice(0, at);
  const plus = value.indexOf("+");
  if (plus >= 0) value = value.slice(0, plus);
  return value;
}

const WORDS = ["amber", "brisk", "calm", "cosmic", "gentle", "lucky", "quiet", "swift", "velvet", "witty"];
const THINGS = ["otter", "falcon", "maple", "comet", "heron", "ember", "puffin", "raven", "lynx", "acorn"];

function randomName(): string {
  const buffer = new Uint32Array(3);
  crypto.getRandomValues(buffer);
  const [a = 0, b = 0, c = 0] = buffer;
  return `${WORDS[a % WORDS.length]}-${THINGS[b % THINGS.length]}-${100 + (c % 900)}`;
}

export function NameBox({ appUrl }: { appUrl: string }) {
  const [value, setValue] = useState("");
  const name = localPart(value);
  const valid = NAME.test(name);

  const go = (target: string) => {
    window.location.href = `${appUrl}/inbox/${encodeURIComponent(target)}`;
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (valid) go(name);
  };

  return (
    <form onSubmit={submit} className="mx-auto flex w-full max-w-xl flex-col gap-2 sm:flex-row" noValidate>
      <label htmlFor="inbox-name" className="sr-only">
        Inbox name
      </label>
      <input
        id="inbox-name"
        value={value}
        onChange={(event) => setValue(event.target.value)}
        placeholder="Type any name"
        autoComplete="off"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        enterKeyHint="go"
        className="address min-h-[3.25rem] min-w-0 flex-1 rounded-full bg-fill-tertiary px-5 type-title3 font-normal text-label outline-none placeholder:font-sans placeholder:text-label-tertiary focus:bg-content focus:ring-[3px] focus:ring-[color-mix(in_srgb,var(--system-blue)_45%,transparent)]"
      />
      <button
        type="submit"
        disabled={!valid}
        className="min-h-[3.25rem] rounded-full bg-tint px-6 type-headline text-white transition-[filter,transform] hover:brightness-110 active:scale-[0.97] disabled:opacity-40"
      >
        Open Inbox
      </button>
      <button
        type="button"
        onClick={() => go(randomName())}
        className="min-h-[3.25rem] rounded-full bg-fill-tertiary px-5 type-headline text-label transition-[background-color,transform] hover:bg-fill-secondary active:scale-[0.97]"
      >
        Random
      </button>
    </form>
  );
}
