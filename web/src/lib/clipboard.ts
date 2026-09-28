import { toast } from "sonner";

/**
 * Copies text and confirms it briefly (Feedback: confirm that a significant
 * action completed). A refusal is said out loud rather than swallowed: the
 * text is on screen and selectable, so the person can still copy it by hand.
 */
export async function copyText(text: string, what: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(`${what} Copied`, { duration: 1600 });
    return true;
  } catch {
    toast.error(`Couldn’t copy. Select the ${what.toLowerCase()} and copy it by hand.`);
    return false;
  }
}

/**
 * Shares an address through the system share sheet where the browser has one
 * (Collaboration and sharing: "use the Share button to display an activity
 * view"), and copies it otherwise.
 */
export async function shareAddress(address: string, url: string): Promise<void> {
  if (typeof navigator.share === "function") {
    try {
      await navigator.share({ title: address, text: address, url });
      return;
    } catch (cause) {
      // Dismissing the share sheet is not an error worth reporting.
      if (cause instanceof DOMException && cause.name === "AbortError") return;
    }
  }
  await copyText(address, "Address");
}

export function canShare(): boolean {
  return typeof navigator !== "undefined" && typeof navigator.share === "function";
}
