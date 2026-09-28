/** Formatting helpers. Nothing here touches the network or the DOM. */

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const timeOnly = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
const weekday = new Intl.DateTimeFormat(undefined, { weekday: "long" });
const shortDate = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const fullDateTime = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  month: "short",
  day: "numeric",
  year: "numeric",
  hour: "numeric",
  minute: "2-digit",
});

/** "just now", "3 minutes ago", "yesterday". */
export function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";

  const seconds = Math.round((then - Date.now()) / 1000);
  const absolute = Math.abs(seconds);

  if (absolute < 45) return "just now";
  if (absolute < 3600) return rtf.format(Math.round(seconds / 60), "minute");
  if (absolute < 86400) return rtf.format(Math.round(seconds / 3600), "hour");
  if (absolute < 604800) return rtf.format(Math.round(seconds / 86400), "day");
  return shortDate.format(new Date(then));
}

/**
 * The time column of a message list, the way Mail shows it: a time today,
 * "Yesterday", a weekday this week, a date before that.
 */
export function listTime(iso: string, now = new Date()): string {
  const then = new Date(iso);
  if (Number.isNaN(then.getTime())) return "";

  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const day = 86_400_000;
  const t = then.getTime();

  if (t >= startOfToday) return timeOnly.format(then);
  if (t >= startOfToday - day) return rtf.format(-1, "day").replace(/^./, (c) => c.toUpperCase());
  if (t >= startOfToday - 6 * day) return weekday.format(then);
  return shortDate.format(then);
}

export function fullDate(iso: string): string {
  const then = new Date(iso);
  return Number.isNaN(then.getTime()) ? "" : fullDateTime.format(then);
}

/** A countdown, in the largest unit that still reads precisely. */
export function countdown(iso: string | undefined, now: number): string {
  if (!iso) return "";
  const remaining = new Date(iso).getTime() - now;
  if (remaining <= 0) return "expired";

  const seconds = Math.floor(remaining / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds % 60}s`;
  return `${seconds}s`;
}

export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** "7 days" for 168, "36 hours" for 36. */
export function retentionLabel(hours: number): string {
  if (hours >= 48 && hours % 24 === 0) return `${hours / 24} days`;
  return `${hours} hours`;
}

/** The host of a URL, for showing where a link goes without printing all of it. */
export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
