// Font picker for the terminal: the monospaced fonts that are installed,
// plus "Eigene…" for anything else.
import { h } from "./ui.js";

const FALLBACK = "Consolas, monospace";
const CUSTOM = "__custom__";

// Common programming fonts; only the installed ones are offered.
const CANDIDATES = [
  "Cascadia Mono", "Cascadia Code", "Consolas", "Lucida Console", "Courier New",
  "JetBrains Mono", "Fira Code", "Fira Mono", "Source Code Pro", "Hack",
  "DejaVu Sans Mono", "Ubuntu Mono", "IBM Plex Mono", "Iosevka", "Roboto Mono",
  "Noto Sans Mono", "Inconsolata", "Victor Mono", "Hasklig", "Menlo",
  "CaskaydiaCove Nerd Font", "CaskaydiaMono Nerd Font", "JetBrainsMono Nerd Font", "MesloLGS NF", "FiraCode Nerd Font",
];

let installed = null;

/** Measures text in each candidate against two generic fonts; a different width means it exists. */
function detect() {
  if (installed) return installed;
  const ctx = document.createElement("canvas").getContext("2d");
  const sample = "mmmmmmmmmmlli1|WW@#0O";
  const width = (font) => { ctx.font = `32px ${font}`; return ctx.measureText(sample).width; };
  const bases = ["monospace", "serif"].map((b) => [b, width(b)]);
  installed = CANDIDATES.filter((name) => bases.some(([b, w]) => width(`"${name}", ${b}`) !== w));
  return installed;
}

/** The first family of a CSS font list, without quotes. */
const primary = (family) => (family || "").split(",")[0].trim().replace(/^["']|["']$/g, "");

/**
 * Returns { el, value() }. value() is the CSS font list to save: the chosen
 * font with a fallback, or the custom text as typed.
 */
export function fontPicker(current) {
  const fonts = detect();
  const select = h("select", {},
    fonts.map((name) => h("option", { value: name, style: { fontFamily: `"${name}", ${FALLBACK}` } }, name)),
    h("option", { value: CUSTOM }, "Eigene…"));
  const custom = h("input", { value: current || "", spellcheck: "false", placeholder: `z. B. "Meine Schrift", ${FALLBACK}` });
  const sample = h("div", { class: "font-sample" }, "Switch#show ip int brief  0O 1lI {}[] |@");

  const own = primary(current);
  select.value = fonts.includes(own) ? own : CUSTOM;
  const shown = select.value;
  const value = () => {
    if (select.value === CUSTOM) return custom.value.trim();
    // Untouched: keep the saved list as it is, with the user's own fallbacks.
    if (select.value === shown && current) return current;
    return `"${select.value}", ${FALLBACK}`;
  };
  const update = () => {
    custom.hidden = select.value !== CUSTOM;
    sample.style.fontFamily = value() || FALLBACK;
  };
  select.addEventListener("change", update);
  custom.addEventListener("input", update);
  update();
  return { el: h("div", { class: "font-picker" }, select, custom, sample), value };
}
