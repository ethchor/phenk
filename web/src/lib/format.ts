/** Formatting helpers. Nothing here touches the network or the DOM. */

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** "just now", "3m ago", "yesterday". */
export function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";

  const seconds = Math.round((then - Date.now()) / 1000);
  const absolute = Math.abs(seconds);

  if (absolute < 45) return "just now";
  if (absolute < 3600) return rtf.format(Math.round(seconds / 60), "minute");
  if (absolute < 86400) return rtf.format(Math.round(seconds / 3600), "hour");
  if (absolute < 604800) return rtf.format(Math.round(seconds / 86400), "day");
  return new Date(then).toLocaleDateString(undefined, { month: "short", day: "numeric" });
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
