// Tile templates for window layouts (same names as store.LayoutTemplates).

export const TEMPLATES = {
  "1": { n: 1, label: "1 Fenster", areas: ['"s0"'], cols: "1fr", rows: "1fr" },
  "2v": { n: 2, label: "2 nebeneinander", areas: ['"s0 s1"'], cols: "1fr 1fr", rows: "1fr" },
  "2h": { n: 2, label: "2 übereinander", areas: ['"s0"', '"s1"'], cols: "1fr", rows: "1fr 1fr" },
  "3m": { n: 3, label: "1 groß + 2 (Master)", areas: ['"s0 s1"', '"s0 s2"'], cols: "1.4fr 1fr", rows: "1fr 1fr" },
  "4": { n: 4, label: "2 × 2", areas: ['"s0 s1"', '"s2 s3"'], cols: "1fr 1fr", rows: "1fr 1fr" },
  "6": { n: 6, label: "3 × 2", areas: ['"s0 s1 s2"', '"s3 s4 s5"'], cols: "1fr 1fr 1fr", rows: "1fr 1fr" },
};

// Display order (object keys like "1"/"4" would otherwise sort first).
export const TEMPLATE_ORDER = ["1", "2v", "2h", "3m", "4", "6"];

/** Inline grid style for a template (used by the workspace and the editor preview). */
export function gridStyle(tpl) {
  const t = TEMPLATES[tpl] || TEMPLATES["1"];
  return { gridTemplateAreas: t.areas.join(" "), gridTemplateColumns: t.cols, gridTemplateRows: t.rows };
}

/** Sensible default: smallest template that fits n sessions. */
export function templateFor(n) {
  if (n <= 1) return "1";
  if (n === 2) return "2v";
  if (n === 3) return "3m";
  if (n === 4) return "4";
  return "6";
}
