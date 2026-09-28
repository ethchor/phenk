import type { Metadata } from "next";
import Link from "next/link";

import { site } from "@/lib/site";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL(site.url),
  title: {
    default: `${site.name} — ${site.tagline}`,
    template: `%s — ${site.name}`,
  },
  description: site.description,
  applicationName: site.name,
  openGraph: {
    type: "website",
    siteName: site.name,
    title: `${site.name} — ${site.tagline}`,
    description: site.description,
    url: site.url,
  },
  twitter: { card: "summary_large_image" },
  alternates: { canonical: "/" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* Appearance follows the system, through the theme's media queries,
            exactly as the app does. There is no stored preference to apply. */}
        <meta name="color-scheme" content="light dark" />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify({
              "@context": "https://schema.org",
              "@type": "SoftwareApplication",
              name: site.name,
              applicationCategory: "CommunicationApplication",
              operatingSystem: "Any",
              description: site.description,
              url: site.url,
              offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
            }),
          }}
        />
      </head>
      <body className="min-h-dvh bg-canvas text-label antialiased">
        <SiteHeader />
        <main>{children}</main>
        <SiteFooter />
      </body>
    </html>
  );
}

function SiteHeader() {
  return (
    // Navigation is the functional layer, so it floats as glass over the page
    // as it scrolls (Materials).
    <header className="sticky top-0 z-30 px-3 pt-3">
      <nav className="glass mx-auto flex max-w-5xl items-center justify-between rounded-full py-2 pl-5 pr-2">
        <Link href="/" className="type-headline">
          phenk<span className="text-tint">.</span>
        </Link>
        <div className="flex items-center gap-1 type-subhead sm:gap-4">
          <Link href="/docs" className="px-2 text-label-secondary hover:text-label">
            Docs
          </Link>
          <Link href="/domains" className="hidden px-2 text-label-secondary hover:text-label sm:inline">
            Domains
          </Link>
          <Link href="/blog" className="hidden px-2 text-label-secondary hover:text-label sm:inline">
            Blog
          </Link>
          <a
            href={site.appUrl}
            className="rounded-full bg-tint px-4 py-2 font-medium text-white transition-[filter] hover:brightness-110"
          >
            Open Inbox
          </a>
        </div>
      </nav>
    </header>
  );
}

function SiteFooter() {
  return (
    <footer className="mt-24 border-t border-separator">
      <div className="mx-auto flex max-w-5xl flex-col gap-4 px-4 py-8 type-subhead text-label-secondary sm:flex-row sm:items-center sm:justify-between">
        <p>
          {site.name} — {site.tagline}.
        </p>
        <div className="flex flex-wrap gap-4">
          <Link href="/privacy" className="hover:text-foreground">
            Privacy
          </Link>
          <Link href="/terms" className="hover:text-foreground">
            Terms
          </Link>
          <a href={site.repo} className="hover:text-foreground" rel="noopener">
            Source
          </a>
        </div>
      </div>
    </footer>
  );
}

