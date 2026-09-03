import type { NextConfig } from "next";

/*
 * The marketing surface.
 *
 * This app never touches Postgres, never imports Go code, and mostly does not
 * talk to anything. It exists to rank: "temporary email" is contested enough
 * that the app surface, which has one route and no indexable content, could
 * never do it.
 *
 * It is deployed separately and is excluded from the Docker image, so a
 * self-hoster ships the inbox without shipping the marketing site with it.
 */
const config: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,

  // The shared component library is TypeScript source rather than a build
  // artifact, so it is compiled here rather than published.
  transpilePackages: ["@phenk/ui"],

  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          // Nothing on this site is interactive enough to need scripting from
          // anywhere but itself.
          {
            key: "Content-Security-Policy",
            value:
              "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
          },
        ],
      },
    ];
  },
};

export default config;
