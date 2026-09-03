import type { Metadata } from "next";

import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: "Privacy",
  description: "What Phenk stores, for how long, and what happens when an address expires.",
  alternates: { canonical: "/privacy" },
};

export default function PrivacyPage() {
  return (
    <div className="mx-auto max-w-2xl px-4 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Privacy</h1>
      <p className="mt-3 text-muted-foreground">
        The short version: {site.name} holds your mail for as long as the address lives and then
        destroys the key it was encrypted under.
      </p>

      <Section title="What is stored">
        <p>
          The messages sent to your address, exactly as they arrived, plus what was derived from
          them: sender, subject, body text, attachments, and the SPF, DKIM and DMARC results. Also
          the connecting server&rsquo;s IP address and the name it announced itself by, which are
          part of the delivery and are what mail authentication is checked against.
        </p>
        <p>
          There is no account. There is no password. There is nothing to sign up for and so nothing
          about you to store.
        </p>
      </Section>

      <Section title="How long">
        <p>
          A generated address lives for the period you chose when you created it. Shortly after it
          expires, its contents are destroyed and the address is retired permanently — it is never
          handed to anyone else.
        </p>
        <p>
          A public inbox is permanent, but its messages are not: each one is removed after a rolling
          retention window, and older messages are also removed when the inbox is full.
        </p>
      </Section>

      <Section title="What destroying means">
        <p>
          Every address has its own encryption key, and everything it receives is encrypted under
          it. Expiry destroys that key. The messages are not marked deleted or moved somewhere
          quieter; they stop being readable, by us as much as by anybody else.
        </p>
      </Section>

      <Section title="Remote images">
        <p>
          Images in a message are fetched by the server rather than by your browser. The sender
          learns nothing about your address, your device, or when you read — which is what an image
          in an email is usually there to find out.
        </p>
      </Section>

      <Section title="In your browser">
        <p>
          One cookie, holding a random value that identifies which addresses are yours. It contains
          nothing about you and is not used for anything else. Your browser also remembers which
          inbox you had open and which theme you prefer.
        </p>
      </Section>

      <Section title="Public inboxes">
        <p>
          A named inbox has no owner and no privacy. Anyone who guesses the name reads the mail.
          Nothing in this policy makes that less true.
        </p>
      </Section>

      <Section title="Requests and disclosure">
        <p>
          We can only produce what we hold at the moment we are asked. For an expired address, that
          is nothing that can be read, and no key exists to change that.
        </p>
      </Section>

      <p className="mt-10 text-sm text-muted-foreground">
        This describes how the software works. An operator running their own {site.name} decides
        their own policy, and the source is public so you can check what it does.
      </p>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mt-10">
      <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
      <div className="mt-2 space-y-3 text-sm text-muted-foreground">{children}</div>
    </section>
  );
}
