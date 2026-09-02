/*
 * Verification code detection.
 *
 * The single most common reason anyone opens a temporary inbox is to read one
 * short code and paste it somewhere else. Lifting it to the top of the message
 * turns a four-step task into a one-step one.
 *
 * The v0 approach is a regular expression pass, and it is deliberately
 * conservative: a wrong code is worse than no code, because someone will paste
 * it, be rejected, and trust the feature less than if it had shown nothing.
 */

/** Words that appear near a real verification code. */
const KEYWORDS =
  /\b(code|c[oó]digo|verify|verification|verifica|otp|one[- ]time|pin|passcode|confirm|confirmation|auth|security|token)\b/i;

/** A plausible code: 4 to 8 characters, digits or upper-case alphanumerics. */
const CANDIDATE = /\b([0-9]{4,8}|[A-Z0-9]{4,8})\b/g;

/** Things that look like codes but are not. */
const EXCLUDED = /^(?:19|20)\d{2}$/; // a bare year

export interface DetectedCode {
  code: string;
  /** The sentence it was found in, for context. */
  context: string;
}

/**
 * Finds the most likely verification code in a message, or null.
 *
 * A candidate only counts when a keyword appears in the same line or the one
 * before it. Scanning the whole message for any 6-digit run would happily
 * return an order number, an amount, or a year.
 */
export function detectVerificationCode(subject: string, body: string): DetectedCode | null {
  const lines = [subject, ...body.split(/\r?\n/)];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    const previous = i > 0 ? (lines[i - 1] ?? "") : "";
    if (!KEYWORDS.test(line) && !KEYWORDS.test(previous)) continue;

    for (const match of line.matchAll(CANDIDATE)) {
      const candidate = match[1];
      if (!candidate || EXCLUDED.test(candidate)) continue;
      // A run of letters that is really a word, like "VERIFY", is not a code.
      if (/^[A-Z]+$/.test(candidate) && KEYWORDS.test(candidate)) continue;
      return { code: candidate, context: line.trim().slice(0, 160) };
    }
  }
  return null;
}
