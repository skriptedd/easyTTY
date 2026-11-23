// Colours words and patterns in terminal output (IP addresses, MACs,
// "err-disabled" …) by wrapping matches in ANSI colour codes before the text
// reaches xterm. Escape sequences of the device are never touched, and the
// device's own colour is restored after each match.

const SGR = { red: 31, green: 32, yellow: 33, blue: 34, magenta: 35, cyan: 36 };

// One complete escape sequence: CSI; OSC, DCS, SOS, PM or APC strings (ended
// by BEL or ST); ESC with intermediates ("ESC ( B"); or a two-byte ESC
// ("ESC 7", "ESC M"). A string that has not ended yet does not match, so a
// window title cut between two chunks is carried over and never coloured.
const ESCAPE = /\x1b(?:\[[0-?]*[ -/]*[@-~]|[\]PX^_][^\x07\x1b]*(?:\x07|\x1b\\)|[ -/]+[0-~]|[0-OQ-WYZ\\`-~])/g;
const SGR_SEQ = /^\x1b\[[0-9;:]*m$/;
const MAX_CARRY = 256; // an unfinished escape longer than this is no escape

// Output arrives in pieces, on slow lines often a few bytes each. So that
// "gro" + "up" is not taken for the word "up", matching sees the end of the
// text before (CONTEXT). A word cut off at the end of a piece waits for the
// rest (see tab.js: until HOLD_MS pass without new output, at most
// HOLD_LIMIT_MS), so "10.0.0.2" + "3" or "up" + "date" are judged whole.
// Only the echo of typed keys is never held, it must show at once.
const CONTEXT = 64;
const HOLD_MAX = 64;
const TRAILING_WORD = /[\w.:/-]+$/; // "/" for 10.0.0.0/24 and Gi1/0/1
export const HOLD_MS = 40;
export const HOLD_LIMIT_MS = 250;

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * Turns the rules from the settings into matchers. Broken expressions are
 * skipped (and reported by the editor, not here).
 */
export function compileRules(rules) {
  const out = [];
  for (const r of rules || []) {
    if (r.off || !r.pattern || !SGR[r.color]) continue;
    let source = r.pattern;
    if (r.kind !== "regex") {
      const words = r.pattern.split(",").map((w) => w.trim()).filter(Boolean);
      if (!words.length) continue;
      // Whole words only; "-" and "." count as part of a word (err-disabled, Gi1/0/1).
      source = `(?<![\\w.-])(?:${words.map(escapeRe).join("|")})(?![\\w-])`;
    }
    try {
      out.push({ re: new RegExp(source, r.kind === "regex" ? "g" : "gi"), sgr: `\x1b[${SGR[r.color]}m` });
    } catch { /* invalid expression: no highlighting for this rule */ }
  }
  return out;
}

/** Checks a rule for the editor: "" when fine, else the reason. */
export function ruleError(rule) {
  if (!rule.pattern.trim()) return "Muster fehlt";
  if (rule.kind !== "regex") return "";
  try {
    const re = new RegExp(rule.pattern, "g");
    if (re.test("")) return "Das Muster passt auch auf leeren Text";
    return "";
  } catch (e) { return "Ungültiger regulärer Ausdruck: " + e.message; }
}

/**
 * Matches of all rules in plain text: non-overlapping, earliest (then longest)
 * first. before is text already shown: it only decides word boundaries.
 */
function spans(text, matchers, before = "") {
  const all = before + text;
  const found = [];
  for (const m of matchers) {
    m.re.lastIndex = 0;
    let hit;
    while ((hit = m.re.exec(all)) !== null) {
      if (hit[0] === "") { m.re.lastIndex++; continue; }
      if (hit.index < before.length) continue; // starts in text that is already out
      found.push({ start: hit.index - before.length, end: hit.index - before.length + hit[0].length, sgr: m.sgr });
    }
  }
  found.sort((a, b) => a.start - b.start || b.end - a.end);
  const out = [];
  let pos = 0;
  for (const f of found) {
    if (f.start >= pos) { out.push(f); pos = f.end; }
  }
  return out;
}

/** Streams terminal text through the rules, one instance per tab. */
export class Highlighter {
  constructor(rules) {
    this.setRules(rules);
    this.carry = ""; // an escape sequence cut off at the end of the last chunk
    this.held = ""; // a word cut off at the end of the last chunk
    this.before = ""; // the end of the plain text shown so far
    this.style = ""; // the device's colour since its last reset, restored after a match
  }

  setRules(rules) {
    this.matchers = compileRules(rules);
  }

  get active() { return this.matchers.length > 0; }

  /** True while a cut-off word waits; call flush() if nothing follows soon. */
  get pending() { return this.held !== ""; }

  /** Returns the waiting word, coloured as far as it can be told. */
  flush() {
    const text = this.held;
    this.held = "";
    this.grew = false;
    return this.plain(text);
  }

  /**
   * Returns text with colour codes around matches. echo: the user just typed,
   * so a piece that is one bare word is likely the echo and is not held back.
   */
  push(chunk, echo = false) {
    const continued = this.held !== ""; // a word held last time goes on here
    let text = this.carry + this.held + chunk;
    this.carry = "";
    this.held = "";
    const cut = text.lastIndexOf("\x1b");
    if (cut >= 0 && text.length - cut < MAX_CARRY) {
      const tail = text.slice(cut);
      ESCAPE.lastIndex = 0;
      const m = ESCAPE.exec(tail);
      if (!m || m.index !== 0) { // the sequence goes on in the next chunk
        this.carry = tail;
        text = text.slice(0, cut);
      }
    }
    let out = "";
    let pos = 0;
    ESCAPE.lastIndex = 0;
    let esc;
    while ((esc = ESCAPE.exec(text)) !== null) {
      out += this.plain(text.slice(pos, esc.index));
      const seq = esc[0];
      if (SGR_SEQ.test(seq)) this.style = seq === "\x1b[m" || seq === "\x1b[0m" ? "" : this.style + seq;
      out += seq;
      pos = esc.index + seq.length;
    }
    let rest = text.slice(pos);
    // Hold back a word cut off at the end. Not the echo of a typed key: that
    // is a piece of one bare word right after typing.
    const word = this.carry ? null : TRAILING_WORD.exec(rest);
    const isEcho = echo && !continued && word?.index + pos === 0;
    this.grew = continued && word?.index + pos === 0; // the same word, longer now
    if (word && word[0].length <= HOLD_MAX && !isEcho) {
      this.held = word[0];
      rest = rest.slice(0, word.index);
    }
    return out + this.plain(rest);
  }

  plain(text) {
    if (!text) return "";
    const hits = spans(text, this.matchers, this.before);
    this.before = (this.before + text).slice(-CONTEXT);
    if (!hits.length) return text;
    let out = "";
    let pos = 0;
    for (const h of hits) {
      out += text.slice(pos, h.start) + h.sgr + text.slice(h.start, h.end) + "\x1b[39m" + this.style;
      pos = h.end;
    }
    return out + text.slice(pos);
  }
}

/** For a preview: text split into parts, each with the colour of its rule ("" = none). */
export function markup(text, rules) {
  const hits = spans(text, compileRules(rules));
  const colorOf = (sgr) => Object.keys(SGR).find((c) => `\x1b[${SGR[c]}m` === sgr) || "";
  const out = [];
  let pos = 0;
  for (const h of hits) {
    if (h.start > pos) out.push({ text: text.slice(pos, h.start), color: "" });
    out.push({ text: text.slice(h.start, h.end), color: colorOf(h.sgr) });
    pos = h.end;
  }
  if (pos < text.length) out.push({ text: text.slice(pos), color: "" });
  return out;
}
