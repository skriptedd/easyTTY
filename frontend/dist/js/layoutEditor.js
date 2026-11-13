// Dialog: pick a tile template for a folder and drag sessions onto its tiles.
import { api, errMsg } from "./api.js";
import { gridStyle, templateFor, TEMPLATE_ORDER, TEMPLATES } from "./layouts.js";
import { h, modal, toast } from "./ui.js";

const SLOT_DRAG = "application/x-easytty-slot";

/**
 * Resolves with {folder, open} after saving, or null.
 * sessions: candidates (the folder's sessions first, then all others).
 */
export function editLayout(folder, sessions) {
  const own = sessions.filter((s) => s.folderId === folder.id);
  const others = sessions.filter((s) => s.folderId !== folder.id);
  let template = folder.layout?.template || templateFor(own.length);
  let slots = folder.layout?.slots?.slice() || own.map((s) => s.id);

  const tplRow = h("div", { class: "tpl-row" });
  const preview = h("div", { class: "tpl-preview" });
  const filter = h("input", { placeholder: "Session suchen…", spellcheck: "false" });
  const sessionList = h("div", { class: "slot-sessions" });

  const name = (id) => sessions.find((s) => s.id === id)?.name;
  const setSlot = (i, id) => { slots = slots.map((v, k) => (k === i ? id : v)); render(); };
  const swapSlots = (a, b) => { slots = slots.map((v, k) => (k === a ? slots[b] : k === b ? slots[a] : v)); render(); };

  // Tiles beyond a smaller raster keep their session, so switching back restores them.
  const shown = () => slots.slice(0, TEMPLATES[template].n);

  const fillFree = (id) => {
    const free = shown().indexOf("");
    if (free < 0) toast("Alle Fenster sind belegt. Zieh die Session auf ein Fenster, um es zu ersetzen.");
    else setSlot(free, id);
  };

  const bindDrop = (cell, i) => {
    cell.addEventListener("dragover", (e) => {
      if (!e.dataTransfer.types.includes(SLOT_DRAG)) return;
      e.preventDefault();
      cell.classList.add("drop");
    });
    cell.addEventListener("dragleave", (e) => { if (!cell.contains(e.relatedTarget)) cell.classList.remove("drop"); });
    cell.addEventListener("drop", (e) => {
      e.preventDefault();
      let id, from;
      try { ({ id, from } = JSON.parse(e.dataTransfer.getData(SLOT_DRAG))); } catch { cell.classList.remove("drop"); return; }
      if (id) setSlot(i, id);
      else if (from !== i) swapSlots(from, i);
      else cell.classList.remove("drop");
    });
  };

  const tile = (id, i) => {
    const cell = h("div", { class: "tpl-cell" + (id ? "" : " free"), style: { gridArea: "s" + i }, draggable: id ? "true" : "false" },
      h("b", {}, i + 1), h("span", {}, name(id) || "Session hierher ziehen"),
      id ? h("button", { class: "tpl-clear", type: "button", title: "Fenster leeren", onclick: () => setSlot(i, "") }, "✕") : null);
    cell.addEventListener("dragstart", (e) => e.dataTransfer.setData(SLOT_DRAG, JSON.stringify({ from: i })));
    bindDrop(cell, i);
    return cell;
  };

  const chip = (s) => {
    const el = h("button", {
      class: "slot-session" + (shown().includes(s.id) ? " used" : ""), type: "button", draggable: "true",
      title: `${s.name}: ziehen oder klicken`, onclick: () => fillFree(s.id),
    }, s.name);
    el.addEventListener("dragstart", (e) => e.dataTransfer.setData(SLOT_DRAG, JSON.stringify({ id: s.id })));
    return el;
  };

  const renderSessions = () => {
    const q = filter.value.trim().toLowerCase();
    const hits = (list) => list.filter((s) => s.name.toLowerCase().includes(q));
    const section = (label, list) => (list.length ? [h("div", { class: "slot-section" }, label), ...list.map(chip)] : []);
    sessionList.replaceChildren(...section(`Ordner „${folder.name}“`, hits(own)), ...section("Andere Sessions", hits(others)));
  };

  const render = () => {
    const n = TEMPLATES[template].n;
    slots = Array.from({ length: Math.max(n, slots.length) }, (_, i) => slots[i] || "");
    tplRow.replaceChildren(...TEMPLATE_ORDER.map((key) => [key, TEMPLATES[key]]).map(([key, t]) => {
      const mini = h("div", { class: "tpl-mini", style: gridStyle(key) },
        Array.from({ length: t.n }, (_, i) => h("span", { style: { gridArea: "s" + i } })));
      return h("button", {
        type: "button", class: "tpl-btn" + (key === template ? " on" : ""), title: t.label,
        onclick: () => { template = key; render(); },
      }, mini, h("span", {}, t.label));
    }));
    Object.assign(preview.style, gridStyle(template));
    preview.replaceChildren(...shown().map(tile));
    renderSessions();
  };
  filter.addEventListener("input", renderSessions);
  render();

  return modal((close) => {
    const save = async (open) => {
      try {
        const saved = await api.SaveFolderLayout(folder.id, { template, slots: shown() });
        close({ folder: saved, open });
      } catch (e) { toast(errMsg(e), "err"); }
    };
    return {
      head: `Fenster-Layout für „${folder.name}“`,
      wide: true,
      sticky: true,
      body: h("div", {},
        h("p", { class: "hint" }, "Raster wählen und Sessions auf die Fenster ziehen. Fenster lassen sich untereinander tauschen. Geöffnet wird alles mit einem Klick auf ▶ am Ordner."),
        tplRow,
        h("div", { class: "layout-edit" }, preview, h("div", { class: "slot-picker" }, filter, sessionList))),
      foot: [
        folder.layout ? h("button", { class: "btn left danger", onclick: async () => {
          try { close({ folder: await api.SaveFolderLayout(folder.id, null), open: false }); }
          catch (e) { toast(errMsg(e), "err"); }
        } }, "Layout entfernen") : null,
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        h("button", { class: "btn", onclick: () => save(false) }, "Speichern"),
        h("button", { class: "btn primary", onclick: () => save(true) }, "Speichern & öffnen"),
      ],
    };
  }).then((v) => v || null);
}
