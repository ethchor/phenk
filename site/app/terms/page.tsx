import type { Metadata } from "next";

import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: "Terms",
  description: `What ${site.name} is for, what it is not for, and what it does not promise.`,
  alternates: { canonical: "/terms" },
};

export default function TermsPage() {
  return (
    <div className="mx-auto max-w-2xl px-4 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Terms</h1>
      <p className="mt-3 text-muted-foreground">
        {site.name} is a temporary inbox. It receives mail; it does not send any.
      </p>

      <Section title="What it is for">
        <p>
          Signups you do not want tied to a permanent address, testing a mail flow you are building,
          and any other case where an address that stops existing is the right shape.
        </p>
      </Section>

      <Section title="What it is not for">
        <ul className="list-disc space-y-1 pl-5">
          <li>
            Anything you need to get back into. Password resets, banking, and account recovery need
            an address that will still be yours next year.
          </li>
          <li>Impersonating somebody, or evading a ban or a block.</li>
          <li>Reading mail intended for another person.</li>
          <li>Automated abuse of whatever is on the other end of the signup.</li>
        </ul>
      </Section>

      <Section title="Public inboxes">
        <p>
          A named inbox is readable by anyone who guesses the name. Do not put anything in one that
          you would mind a stranger reading, and do not go looking through other people&rsquo;s.
        </p>
      </Section>

      <Section title="What is not promised">
        <p>
          There is no guarantee that a message will arrive, that it will arrive within any
          particular time, or that it will still be there when you look. Addresses expire, mail gets
          filtered, and domains get blocklisted. Do not build anything on the assumption that a
          message is safe here.
        </p>
        <p>
          The service is provided as it is, with no warranty of any kind, and liability is limited
          to the fullest extent the law allows.
        </p>
      </Section>

      <Section title="Abuse">
        <p>
          Addresses used for abuse are destroyed, and the domains they were on may be withdrawn.
          There is no appeal, because there is no account to appeal about.
        </p>
      </Section>

      <p className="mt-10 text-sm text-muted-foreground">
        These terms describe the reference deployment. An operator running their own copy sets their
        own.
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
