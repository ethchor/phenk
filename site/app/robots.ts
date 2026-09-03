import type { MetadataRoute } from "next";

import { site } from "@/lib/site";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      // The app surface has one route, no indexable content, and live inboxes
      // behind it. Nothing there belongs in a search result.
      disallow: ["/api/"],
    },
    sitemap: `${site.url}/sitemap.xml`,
  };
}
