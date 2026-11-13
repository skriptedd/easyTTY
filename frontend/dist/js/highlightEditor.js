// Settings block for terminal highlighting: one row per rule (words or a
// regular expression, a colour), a live preview, defaults on request.
import { api, errMsg } from "./api.js";
import { h, toast } from "./ui.js";
import { markup, ruleError } from "./highlight.js";

const COLORS = [["red", "Rot"], ["green", "Grün"], ["yellow", "Gelb"], ["blue", "Blau"], ["magenta", "Magenta"], ["cyan", "Türkis"]];

const SAMPLE = [
  "Gi1/0/1    10.0.10.1       YES manual up                    up",
  "Gi1/0/2    unassigned      YES unset  down                  down",
  "  10    0050.56a1.2b3c    DYNAMIC     Gi1/0/1",
  "%PM-4-ERR_DISABLE: bpduguard error detected on Gi1/0/7, putting Gi1/0/7 in err-disabled state",
  "64 bytes from 10.10.10.1: icmp_seq=1 ttl=64 · MAC 00:1a:2b:3c:4d:5e",
].join("\n");

/** Returns { elements, collect() }; collect throws when a rule is broken. */
export function highlightFields(s) {
  let rows = (s.highlights || []).map((r) => ({ ...r }));
  const list = h("div", { class: "hl-list" });
  const preview = h("pre", { class: "hl-preview" });

  const renderPreview = () => {
    preview.replaceChildren(...markup(SAMPLE, rows).map((p) => (p.color ? h("span", { class: "hl-" + p.color }, p.text) : p.text)));
  };

  const render = () => {
    list.replaceChildren(...rows.map((row, i) => {
      const on = h("input", { type: "checkbox", checked: !row.off, title: "Regel an/aus" });
      const name = h("input", { value: row.name, placeholder: "Name", spellcheck: "false", class: "hl-name" });
      const kind = h("select", { title: "Wörter: Kommaliste, ganze Wörter, Groß/klein egal. Regex: regulärer Ausdruck." },
        h("option", { value: "words" }, "Wörter"), h("option", { value: "regex" }, "Regex"));
      kind.value = row.kind === "regex" ? "regex" : "words";
      const pattern = h("input", { value: row.pattern, spellcheck: "false", class: "hl-pattern",
        placeholder: kind.value === "regex" ? "z. B. \\bVLAN\\d+\\b" : "z. B. down, err-disabled" });
      const color = h("select", { class: "hl-color" }, COLORS.map(([v, l]) => h("option", { value: v, class: "hl-" + v }, l)));
      color.value = row.color || "red";
      color.className = "hl-color hl-" + color.value;
      const err = h("div", { class: "hl-err" });
      const check = () => {
        const msg = ruleError(row);
        err.textContent = msg;
        pattern.classList.toggle("bad", !!msg);
      };
      const changed = () => { check(); renderPreview(); };
      on.addEventListener("change", () => { row.off = !on.checked; changed(); });
      name.addEventListener("input", () => { row.name = name.value; });
      kind.addEventListener("change", () => { row.kind = kind.value; changed(); });
      pattern.addEventListener("input", () => { row.pattern = pattern.value; changed(); });
      color.addEventListener("change", () => { row.color = color.value; color.className = "hl-color hl-" + color.value; changed(); });
      check();
      return h("div", { class: "hl-row" },
        h("div", { class: "hl-line" }, on, name, kind, pattern, color,
          h("button", { class: "btn danger", type: "button", title: "Regel löschen", onclick: () => { rows = rows.filter((_, k) => k !== i); render(); } }, "✕")),
        err);
    }));
    if (!rows.length) list.append(h("p", { class: "hint" }, "Keine Regeln – das Terminal zeigt alles in den Farben des Geräts."));
    renderPreview();
  };
  render();

  return {
    elements: [
      h("div", { class: "full" }, list),
      h("div", { class: "full share-row" },
        h("button", { class: "btn", type: "button", onclick: () => {
          rows = [...rows, { name: "", kind: "words", pattern: "", color: "yellow", off: false }];
          render();
          list.querySelector(".hl-row:last-child .hl-pattern")?.focus();
        } }, "+ Regel"),
        h("button", { class: "btn", type: "button", onclick: async () => {
          try { rows = (await api.DefaultHighlights()).map((r) => ({ ...r })); render(); } catch (e) { toast(errMsg(e), "err"); }
        } }, "Standard wiederherstellen")),
      h("div", { class: "full" }, h("div", { class: "hint" }, "Vorschau"), preview),
      h("div", { class: "full hint" }, "Färbt nur die Anzeige, Logs und gespeicherte Ausgaben bleiben unverändert. Bei Treffern mehrerer Regeln gewinnt der früheste."),
    ],
    collect: () => {
      const broken = rows.find((r) => !r.off && ruleError(r));
      if (broken) throw new Error(`Hervorhebung „${broken.name || broken.pattern}“: ${ruleError(broken)}`);
      return { highlights: rows.filter((r) => r.pattern.trim()).map((r) => ({ name: r.name.trim(), kind: r.kind, pattern: r.pattern.trim(), color: r.color, off: !!r.off })) };
    },
  };
}
