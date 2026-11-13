// Colours words and patterns in terminal output (IP addresses, MACs,
// "err-disabled" …) by wrapping matches in ANSI colour codes before the text
// reaches xterm. Escape sequences of the device are never touched, and the
// device's own colour is restored after each match.

const SGR = { red: 31, green: 32, yellow: 33, blue: 34, magenta: 35, cyan: 36 };

// One complete escape sequence: CSI, OSC (BEL or ST), or a two-byte ESC.
const ESCAPE = /\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])/g;
const SGR_SEQ = /^\x1b\[[0-9;:]*m$/;
const MAX_CARRY = 256; // an unfinished escape longer than this is no escape

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

/** Matches of all rules in plain text: non-overlapping, earliest (then longest) first. */
function spans(text, matchers) {
  const found = [];
  for (const m of matchers) {
    m.re.lastIndex = 0;
    let hit;
    while ((hit = m.re.exec(text)) !== null) {
      if (hit[0] === "") { m.re.lastIndex++; continue; }
      found.push({ start: hit.index, end: hit.index + hit[0].length, sgr: m.sgr });
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
    this.style = ""; // the device's colour since its last reset, restored after a match
  }

  setRules(rules) {
    this.matchers = compileRules(rules);
  }

  get active() { return this.matchers.length > 0; }

  /** Returns text with colour codes around matches. */
  push(chunk) {
    let text = this.carry + chunk;
    this.carry = "";
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
    return out + this.plain(text.slice(pos));
  }

  plain(text) {
    if (!text) return "";
    const hits = spans(text, this.matchers);
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
