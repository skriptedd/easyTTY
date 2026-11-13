// Sharing with colleagues: export/import sessions, folders (with layouts)
// and settings. User names, passwords and personal paths are never exported.
import { api, errMsg } from "./api.js";
import { h, modal, toast } from "./ui.js";

/** scope: { folderIds?, sessionIds?, all?, label } */
export async function exportShare(scope) {
  const withSettings = h("input", { type: "checkbox", checked: !!scope.all || !!scope.settingsOnly, disabled: !!scope.settingsOnly });
  const ok = await modal((close) => ({
    head: "Exportieren zum Teilen",
    body: h("div", { class: "grid" },
      h("p", { class: "full" }, `Was: ${scope.label}`),
      h("label", { class: "check full" }, withSettings, "Einstellungen mit exportieren (Design, Kürzel, Namensschema, Einfügen …)"),
      h("p", { class: "full hint" },
        "Nicht enthalten: Benutzernamen, Passwörter, Schlüsseldateien, Log-Pfade. Fenster-Layouts kommen mit, soweit ihre Sessions dabei sind.")),
    foot: [
      h("button", { class: "btn", onclick: () => close(false) }, "Abbrechen"),
      h("button", { class: "btn primary", onclick: () => close(true) }, "Speichern unter…"),
    ],
  }));
  if (!ok) return;
  try {
    const path = await api.ExportShare(scope.folderIds || [], scope.sessionIds || [], !!scope.all, withSettings.checked);
    if (path) toast(`Exportiert: ${path}`, "ok", 7000);
  } catch (e) { toast(errMsg(e), "err"); }
}

/** Resolves true when something was imported (caller reloads tree/settings). */
export async function importShare() {
  let p;
  try { p = await api.PickShare(); } catch (e) { toast(errMsg(e), "err"); return false; }
  if (!p) return false;
  const withSettings = h("input", { type: "checkbox", checked: false });
  const ok = await modal((close) => ({
    head: "Import aus easyTTY-Export",
    body: h("div", { class: "grid" },
      h("p", { class: "full" }, `${p.sessions} Session(s), ${p.folders} Ordner · exportiert am ${p.exported}`),
      p.hasSettings ? h("label", { class: "check full" }, withSettings, "Auch die Einstellungen übernehmen (dein Log-Ordner bleibt)") : null,
      h("p", { class: "full hint" },
        "Gleichnamige Ordner werden zusammengeführt, schon vorhandene Sessions übersprungen – ein erneuter Import doppelt also nichts. Benutzer und Passwort trägst du danach selbst ein.")),
    foot: [
      h("button", { class: "btn", onclick: () => close(false) }, "Abbrechen"),
      h("button", { class: "btn primary", onclick: () => close(true) }, "Importieren"),
    ],
  }));
  if (!ok) return false;
  try {
    const r = await api.ApplyShare(p.path, withSettings.checked);
    toast(`Importiert: ${r.sessions} Session(s), ${r.folders} neue Ordner${r.skipped ? `, ${r.skipped} schon vorhanden` : ""}${r.settingsApplied ? ", Einstellungen übernommen" : ""}.`, "ok", 7000);
    return true;
  } catch (e) { toast("Import: " + errMsg(e), "err"); return false; }
}
