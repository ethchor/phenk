import fs from "node:fs";
import path from "node:path";

/*
 * The blog.
 *
 * Posts are Markdown files with a small front matter block, read at build time.
 * "Temporary email" is a contested enough search term that the homepage alone
 * will not rank for it, and this is the surface that can.
 *
 * The renderer is deliberately small rather than a Markdown library: the posts
 * are written in this repository by people who can be trusted, the subset in
 * use is narrow, and a dependency that parses arbitrary Markdown is a lot of
 * surface for a handful of pages.
 */

export interface Post {
  slug: string;
  title: string;
  description: string;
  date: string;
  body: string;
}

const CONTENT = path.join(process.cwd(), "content", "blog");

export function listPosts(): Post[] {
  if (!fs.existsSync(CONTENT)) return [];

  return fs
    .readdirSync(CONTENT)
    .filter((name) => name.endsWith(".md"))
    .map((name) => readPost(name.replace(/\.md$/, "")))
    .filter((post): post is Post => post !== null)
    .sort((a, b) => b.date.localeCompare(a.date));
}

export function readPost(slug: string): Post | null {
  // The slug reaches this function from a URL, so it is checked rather than
  // trusted: anything but a plain name could walk out of the content directory.
  if (!/^[a-z0-9-]+$/.test(slug)) return null;

  const file = path.join(CONTENT, `${slug}.md`);
  if (!fs.existsSync(file)) return null;

  const raw = fs.readFileSync(file, "utf8");
  const match = /^---\n([\s\S]*?)\n---\n([\s\S]*)$/.exec(raw);
  if (!match) return null;

  const [, frontMatter = "", body = ""] = match;
  const meta: Record<string, string> = {};
  for (const line of frontMatter.split("\n")) {
    const separator = line.indexOf(":");
    if (separator < 0) continue;
    meta[line.slice(0, separator).trim()] = line
      .slice(separator + 1)
      .trim()
      .replace(/^["']|["']$/g, "");
  }

  return {
    slug,
    title: meta.title ?? slug,
    description: meta.description ?? "",
    date: meta.date ?? "1970-01-01",
    body: body.trim(),
  };
}
