/** Everything about the deployment that appears in more than one place. */
export const site = {
  name: "Phenk",
  tagline: "Any name is an inbox",
  description:
    "Public email inboxes you open by typing a name — no sign-up, no password. For people who want one less signup, test suites that need a real inbox, and agents that need to read a verification email.",
  url: process.env.NEXT_PUBLIC_SITE_URL ?? "https://phenk.example",
  appUrl: process.env.NEXT_PUBLIC_APP_URL ?? "https://app.phenk.example",
  repo: "https://github.com/ethchor/phenk",
} as const;

/** An absolute URL, for metadata that cannot use a relative one. */
export function absolute(path: string): string {
  return new URL(path, site.url).toString();
}
