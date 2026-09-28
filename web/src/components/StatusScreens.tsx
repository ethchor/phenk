import type { ReactNode } from "react";
import { CloudOff } from "lucide-react";
import { Button } from "@phenk/ui";

import { EmptyState } from "./EmptyState";

/**
 * The launch screen: the empty canvas and nothing else. The Launching page
 * asks for a launch screen that is nearly identical to the first screen, with
 * no text and no branding, so the app appears to open instantly rather than
 * after an announcement.
 */
export function LaunchScreen() {
  return <div className="min-h-dvh bg-canvas" aria-busy="true" />;
}

export function ServerUnavailable() {
  return (
    <FullScreen>
      <EmptyState
        icon={<CloudOff />}
        title="Phenk Isn’t Responding"
        actions={<Button onClick={() => window.location.reload()}>Try Again</Button>}
      >
        The server didn’t answer. Mail sent in the meantime is kept once it is back.
      </EmptyState>
    </FullScreen>
  );
}

export function FullScreen({ children }: { children: ReactNode }) {
  return <main className="flex min-h-dvh items-center justify-center bg-canvas p-5">{children}</main>;
}
