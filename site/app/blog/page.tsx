import type { Metadata } from "next";
import Link from "next/link";

import { listPosts } from "@/lib/blog";

export const metadata: Metadata = {
  title: "Blog",
  description: "Notes on disposable email, deliverability, and building Phenk.",
  alternates: { canonical: "/blog" },
};

export const dynamic = "force-static";

export default function BlogIndex() {
  const posts = listPosts();

  return (
    <div className="mx-auto max-w-3xl px-4 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Blog</h1>
      <p className="mt-3 text-muted-foreground">
        Notes on disposable email, deliverability, and building this thing.
      </p>

      {posts.length === 0 ? (
        <p className="mt-10 text-sm text-muted-foreground">Nothing published yet.</p>
      ) : (
        <ul className="mt-10 space-y-6">
          {posts.map((post) => (
            <li key={post.slug}>
              <Link href={`/blog/${post.slug}`} className="group block">
                <h2 className="text-lg font-medium group-hover:text-primary">{post.title}</h2>
                <p className="mt-1 text-sm text-muted-foreground">{post.description}</p>
                <time className="mt-1 block text-xs text-muted-foreground" dateTime={post.date}>
                  {new Date(post.date).toLocaleDateString(undefined, {
                    year: "numeric",
                    month: "long",
                    day: "numeric",
                  })}
                </time>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
