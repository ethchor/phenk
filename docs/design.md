# Design

The inbox app follows Apple's Human Interface Guidelines, adapted for the web.
This page records which guidance applies, the concrete values taken from it,
and the decisions that follow — so a change to the UI can be checked against
something more durable than taste.

## Sources, and how current they are

- **Apple Human Interface Guidelines** —
  <https://developer.apple.com/design/human-interface-guidelines>. All six
  sections were read: Getting started, Foundations, Patterns, Components,
  Inputs and Technologies, 172 pages in total. The HIG carries a change log on
  every page; the most recent entries are dated **September 17, 2026**, and
  the June 8, 2026 round covers the 27 releases (design principles
  reintroduced, sidebar and search terminology, scroll edge effects, menu item
  icons, generative AI feedback). It is the current source of truth.
- **Xcode 27 skills** — <https://github.com/superagents-lab/xcode27-skills>, an
  export of the agent skills Apple ships in Xcode 27, dated June 9, 2026. It
  covers SwiftUI, UIKit modernization, Swift Testing, C bounds safety and
  build hardening. It contains **no** HIG or visual-design content, and is
  older than the HIG's latest revisions, so where the two touch the HIG wins.
  Its only design-relevant material is behavioural, and is reflected below:
  toolbar items overflow into a menu by priority, a trailing toolbar item can
  be pinned so it never overflows, toolbars minimize on scroll, and rows in
  any scrolling list can carry swipe actions.

The app is a web page, not a native app, so three things are adapted rather
than copied. SF Pro and SF Symbols are licensed for Apple platforms only, so
the app uses the system font stack — which *is* SF on Apple devices — and a
line-icon set drawn to match SF Symbols' regular weight. Liquid Glass is
approximated with `backdrop-filter`. And there is no Dynamic Type API, so type
is set in `rem` and scales with the browser's text size.

## Principles that decide arguments

From *Design principles* (June 2026), the ones this product leans on:

- **Stay out of the way / Include just what's necessary.** The job is: type a
  name, read the mail, copy a code. Everything on screen serves one of those.
- **Be fully transparent about what your product does.** Public inboxes are
  readable by anyone who knows the name. The app says so plainly, once, where
  the name is chosen, and never buries it.
- **Help people recover from mistakes.** Deleting a message is undoable
  instead of confirmed (see *Alerts* below).
- **Preserve a person's context.** The last inbox reopens on launch.

## Foundations

**Color.** The system palette, as published on the *Color* page, with its
increased-contrast variants used under `prefers-contrast: more`. The accent is
system Blue — `rgb(0 136 255)` light, `rgb(0 145 255)` dark. Labels use the
four-level label hierarchy (label, secondary, tertiary, quaternary) rather
than ad-hoc greys. Color is never the only signal: the public notice has an
icon and text, focus has a ring and not just a tint, and authentication
results carry words.

**Dark Mode.** *"Avoid offering an app-specific appearance setting."* The app
follows the system appearance and has no theme toggle. HTML mail is rendered
on its own white page, dimmed slightly in dark mode, per *"soften the color of
white backgrounds."*

**Typography.** The text styles and sizes come from the *Typography* tables.
On touch devices the iOS scale applies (Body 17pt, Headline 17pt semibold,
Subhead 15, Footnote 13, Caption 12); with a fine pointer the macOS-derived
scale applies, nudged up for a browser window (Body 14). Light weights are
avoided. The address uses a monospaced face so `0` and `O` cannot be confused.

**Layout.** Content is ordered by importance and grouped; controls are
visually distinct from content; layout adapts by available width, not by
device. Safe-area insets are respected. Touch targets are at least **44×44pt**
(the iOS default control size); with a fine pointer controls may be 28pt, the
macOS default.

**Materials.** *"Don't use Liquid Glass in the content layer."* Glass is used
only for the functional layer — the toolbar and the sidebar — floating above
content, in the **regular** variant (blur plus a luminosity shift). The
message list and the message itself are opaque content. Under
`prefers-reduced-transparency` glass becomes a solid surface.

**Motion.** Brief, purposeful, and optional. Frequent interactions — selecting
a message, typing — are not animated. Everything respects
`prefers-reduced-motion`.

**Writing.** Title case for buttons and menu items, sentence case for
everything else. Every empty screen says what to do next. Error messages say
what happened and what to try.

**Privacy.** Remote images are never loaded directly (the server proxies
them); the app says so when a message is opened. Nothing but inbox names is
stored in the browser.

## Patterns

- **Launching.** Launch instantly; no splash screen and no onboarding flow —
  the name box teaches the product by being used. Installed as an app, Phenk
  restores the last inbox. In a browser tab it opens on the name field, with
  recent inboxes listed first: a visit to the root is how someone starts
  something new, so the front door must stay reachable, and continuing is
  still one tap.
- **Entering data.** The name field validates as you type, shows the full
  address that will result, and never autocorrects or capitalizes.
- **Searching.** The inbox filter starts as soon as you type, lives in the
  toolbar, and says what it searches.
- **Loading.** Show structure immediately; an inbox waiting for mail says it
  is waiting, live.
- **Feedback.** Copying shows a brief confirmation. A command that cannot run
  says why.
- **Undo and redo.** Destructive actions on messages are undoable for a few
  seconds rather than confirmed up front.
- **Settings.** As few as possible. Appearance is the system's.
- **Offering only what can succeed.** Private addresses are offered only when
  the server enables them *and* has a domain able to hand one out; a button
  that can only fail is worse than no button.
- **Collaboration and sharing.** A Share button opens the system share sheet
  (Web Share API) where available, and copies the address otherwise.

## Components

- **Split view.** Three panes at wide widths — sidebar, message list,
  message — two at medium widths, one at a time on a phone. The current
  selection stays highlighted in every pane that leads to the detail.
- **Sidebar.** Recent inboxes and the domains on offer. Two levels at most,
  familiar symbols, hideable.
- **Toolbar.** A floating glass bar. The window title is the inbox, never the
  app name. Items are chosen to avoid overflow; the address' copy action is
  the pinned trailing item.
- **Lists.** Rows are sender, subject, preview and time — succinct, with the
  selection highlighted rather than ringed.
- **Buttons.** One prominent (filled) button for the most likely action on a
  screen. A destructive action is never the primary one. Every custom button
  has a pressed state.
- **Menus and context menus.** Title case, icons used sparingly, an ellipsis
  when more input is needed. Every context-menu action also exists in the main
  interface.
- **Alerts.** *"Avoid displaying alerts for common, undoable actions, even
  when they're destructive."* The app has none for deleting a message.
- **Segmented control.** Switches a message between its formatted and plain
  text views; nouns, two segments.
- **Labels.** The address is selectable text, because people copy it by hand.
- **Web views.** HTML mail renders in a sandboxed iframe with scripting and
  same-origin access both disabled.

## Inputs

- **Keyboards.** Standard shortcuts are never repurposed — in particular ⌘R
  stays the browser's reload. App shortcuts are single keys that do not clash
  with text entry: `/` focuses the filter, `j`/`k` or the arrow keys move
  through messages, `c` copies the detected code, `Escape` leaves a message.
- **Focus and selection.** A focus ring for text fields and buttons, a
  highlight for the selected list row, and focus never moves without the
  person's action.
- **Pointing devices.** Hover reveals, it does not hide. Controls that
  minimize are restored by pointer movement.

## Technologies: detected codes

Verification codes and account links are detected by rules, not by a model,
but the *Generative AI* and *Machine learning* guidance still applies to
anything that guesses on a person's behalf: say where a result came from, set
expectations, and let people check it. The detected code is labelled as
detected and always shown with the line it came from, so a reader can see it
is right before pasting it.
