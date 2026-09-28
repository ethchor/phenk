import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@phenk/ui";

const SHORTCUTS: Array<[string[], string]> = [
  [["/"], "Filter messages"],
  [["J", "↓"], "Next message"],
  [["K", "↑"], "Previous message"],
  [["C"], "Copy the detected code"],
  [["A"], "Copy the address"],
  [["Esc"], "Clear the filter, or close the message"],
];

/**
 * The keyboard shortcuts, listed where people can find them. Single keys only,
 * none that clash with the browser's own (Keyboards: "respect standard
 * keyboard shortcuts").
 */
export function ShortcutsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showClose className="max-w-[24rem]">
        <DialogHeader className="text-left">
          <DialogTitle>Keyboard Shortcuts</DialogTitle>
          <DialogDescription>They work anywhere in the inbox except while you are typing.</DialogDescription>
        </DialogHeader>
        <dl className="mt-4 flex flex-col gap-2.5">
          {SHORTCUTS.map(([keys, label]) => (
            <div key={label} className="flex items-center justify-between gap-4">
              <dt className="type-body text-label">{label}</dt>
              <dd className="flex gap-1">
                {keys.map((key) => (
                  <kbd
                    key={key}
                    className="min-w-[1.75rem] rounded-md bg-fill-tertiary px-1.5 py-0.5 text-center font-sans type-subhead text-label shadow-[inset_0_-1px_0_var(--separator)]"
                  >
                    {key}
                  </kbd>
                ))}
              </dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  );
}
