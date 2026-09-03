/** Everything about the deployment that appears in more than one place. */
export const site = {
  name: "Phenk",
  tagline: "Email addresses that expire",
  description:
    "Disposable email for people who want one less signup, developers who want a test inbox, and agents that need to receive mail without a mailbox.",
  url: process.env.NEXT_PUBLIC_SITE_URL ?? "https://phenk.example",
  appUrl: process.env.NEXT_PUBLIC_APP_URL ?? "https://app.phenk.example",
  repo: "https://github.com/ethchor/phenk",
} as const;

/** An absolute URL, for metadata that cannot use a relative one. */
export function absolute(path: string): string {
  return new URL(path, site.url).toString();
}
