// One keymap for the whole app. Every app shortcut is Alt+key (one layer,
// with Alt instead of Super, which Windows reserves), so Strg combos stay
// with the terminal. Only Strg+Tab remains: Alt+Tab belongs to Windows.

const ARROWS = { ArrowLeft: "left", ArrowRight: "right", ArrowUp: "up", ArrowDown: "down" };

// Alt + key (no Ctrl, so AltGr characters like @ { [ still type normally).
const ALT = {
  enter: "quick", t: "quick",
  w: "close",
  f: "zoom",
  d: "duplicate",
  b: "sftp",
  g: "search",
  s: "sidebar",
  k: "help",
  c: "copy",
  v: "paste",
  l: "log",
  i: "status",
  e: "tree",
  0: "terminal",
  u: "firmware",
  a: "tasks",
  j: "snippets",
};

// Font size: Alt+Plus / Alt+Minus, Alt+= (Shift+0 on German keyboards) resets.
const FONT = { "+": "font:+", "-": "font:-", "=": "font:0" };

/**
 * Maps a keydown to an app action ("close", "tab:3", "dir:left", …) or null.
 * altKeys=false leaves every Alt combo to the terminal (bash uses Alt+b/f/d…).
 */
export function keyAction(e, { altKeys = true } = {}) {
  if (e.type !== "keydown") return null;
  const k = e.key.toLowerCase();
  if (altKeys && e.altKey && !e.ctrlKey && !e.metaKey) {
    if (FONT[e.key]) return FONT[e.key];
    if (ARROWS[e.key] && !e.shiftKey) return "dir:" + ARROWS[e.key];
    if (/^[1-9]$/.test(e.key) && !e.shiftKey) return "tab:" + e.key;
    if (!e.shiftKey && ALT[k]) return ALT[k];
  }
  if (e.ctrlKey && !e.altKey && k === "tab") return e.shiftKey ? "prev" : "next";
  // Alt shortcuts switched off (Alt belongs to bash, vim …): the same letters
  // work with Strg+Shift, so nothing is mouse-only.
  if (!altKeys && e.ctrlKey && e.shiftKey && !e.altKey && !e.metaKey && k.length === 1 && ALT[k]) return ALT[k];
  return null;
}

/** Actions that also work while typing in a form field (quick connect etc.). */
export const GLOBAL_IN_FIELDS = new Set(["quick", "sidebar", "help", "status", "firmware", "tasks", "tree", "terminal", "close", "zoom", "next", "prev"]);
