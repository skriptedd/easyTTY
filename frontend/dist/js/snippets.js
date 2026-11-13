// Command snippets: often used commands sent to the active tab with one click
// (button "Schnipsel" in the tab bar or Alt+J). Stored in the settings.
import { api, errMsg } from "./api.js";
import { contextMenu, h, modal, toast } from "./ui.js";

/**
 * Shows the snippet menu at x/y. send(snippet) runs it in the tab,
 * manage() opens the editor.
 */
export function snippetMenu(x, y, snippets, { send, manage, canSend, onCancel }) {
  const list = snippets || [];
  const items = list.map((sn, i) => ({
    label: sn.saveOutput ? `${sn.name}  → Datei` : sn.name,
    hint: i < 9 ? `${i + 1}` : (sn.text.includes("\n") ? `${sn.text.split("\n").length} Zeilen` : ""),
    disabled: !canSend,
    action: () => send(sn),
  }));
  if (!list.length) items.push({ label: "Noch keine Schnipsel", disabled: true, action: () => {} });
  items.push("-", { label: "Schnipsel verwalten…", action: manage });
  const menu = contextMenu(x, y, items);
  // Keyboard: digits pick a snippet, arrows move, the first entry has the focus.
  const buttons = [...menu.querySelectorAll("button:not(:disabled)")];
  buttons[0]?.focus();
  menu.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { onCancel?.(); return; } // the menu had the keyboard: hand it back
    if (/^[1-9]$/.test(e.key) && list[+e.key - 1] && canSend) {
      e.preventDefault();
      menu.querySelectorAll("button")[+e.key - 1].click();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const i = buttons.indexOf(document.activeElement);
    const next = e.key === "ArrowDown" ? (i + 1) % buttons.length : (i - 1 + buttons.length) % buttons.length;
    buttons[next].focus();
  });
}

/** Editor for all snippets. Resolves with the saved settings, or null. */
export function manageSnippets(current) {
  let rows = (current || []).map((sn) => ({ ...sn }));
  const list = h("div", { class: "snippet-list" });

  const render = () => {
    list.replaceChildren(...rows.map((row, i) => {
      const name = h("input", { value: row.name, placeholder: "Name, z. B. Uhrzeit", spellcheck: "false" });
      const text = h("textarea", { rows: Math.min(6, Math.max(2, row.text.split("\n").length)), spellcheck: "false", placeholder: "Befehl, mehrere Zeilen gehen nacheinander raus" }, row.text);
      name.addEventListener("input", () => { row.name = name.value; });
      text.addEventListener("input", () => { row.text = text.value; });
      const save = h("input", { type: "checkbox", checked: !!row.saveOutput });
      save.addEventListener("change", () => { row.saveOutput = save.checked; });
      const move = (d) => {
        const j = i + d;
        if (j < 0 || j >= rows.length) return;
        rows = rows.map((r, k) => (k === i ? rows[j] : k === j ? rows[i] : r));
        render();
      };
      return h("div", { class: "snippet-row" },
        h("div", { class: "snippet-head" },
          h("span", { class: "muted" }, i < 9 ? `${i + 1}` : ""), name,
          h("label", { class: "check snippet-save", title: "Befehle nacheinander ausführen und die Ausgabe als Textdatei im Config-Ordner speichern" }, save, "Ausgabe in Datei"),
          h("button", { class: "btn", type: "button", title: "Nach oben", disabled: i === 0, onclick: () => move(-1) }, "↑"),
          h("button", { class: "btn", type: "button", title: "Nach unten", disabled: i === rows.length - 1, onclick: () => move(1) }, "↓"),
          h("button", { class: "btn danger", type: "button", title: "Schnipsel löschen", onclick: () => { rows = rows.filter((_, k) => k !== i); render(); } }, "✕")),
        text);
    }));
    if (!rows.length) list.append(h("p", { class: "hint" }, "Noch keine Schnipsel. Leg einen an, zum Beispiel „show clock“."));
  };
  render();

  return modal((close) => ({
    head: "Befehls-Schnipsel",
    wide: true,
    sticky: true,
    body: h("div", {},
      h("p", { class: "hint" }, "Ein Klick im Menü „Schnipsel“ (Alt+J) tippt den Befehl in den aktiven Tab und drückt Enter. Mehrere Zeilen gehen wie beim Einfügen Zeile für Zeile raus. Mit „Ausgabe in Datei“ führt easyTTY die Befehle nacheinander aus, blättert selbst um und speichert alles als Textdatei im Config-Ordner (Einstellungen → Running-Configs). Die ersten neun haben die Ziffern 1 bis 9."),
      list,
      h("button", { class: "btn", type: "button", onclick: () => {
        rows = [...rows, { name: "", text: "" }];
        render();
        list.querySelector(".snippet-row:last-child input")?.focus();
      } }, "+ Neuer Schnipsel")),
    foot: [
      h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
      h("button", { class: "btn primary", onclick: async () => {
        try {
          const cur = await api.GetSettings();
          close(await api.SaveSettings({ ...cur, mcpToken: "", snippets: rows.map((r) => ({ name: r.name.trim(), text: r.text, saveOutput: !!r.saveOutput })) }));
        } catch (e) { toast(errMsg(e), "err"); }
      } }, "Speichern"),
    ],
  })).then((v) => v || null);
}
