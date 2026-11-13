// "Für alle Sessions ändern": one change for every session of a folder (or of
// the whole tree) – user, password, SSH login, key, keepalive, colour.
import { api, errMsg } from "./api.js";
import { COLORS } from "./dialogs.js";
import { h, modal, toast } from "./ui.js";

/** Sessions of a folder ("" = top level), with subfolders if recursive. */
function inScope(tree, folderId, recursive) {
  const ids = new Set([folderId]);
  if (recursive) {
    for (let grew = true; grew;) {
      grew = false;
      for (const f of tree.folders) if ((ids.has(f.parentId) || (folderId === "" && f.parentId === "")) && !ids.has(f.id)) { ids.add(f.id); grew = true; }
    }
  }
  return tree.sessions.filter((s) => ids.has(s.folderId));
}

/**
 * Opens the dialog for a folder (null = "Alle Sessions", the global level).
 * Resolves with the number of changed sessions, or 0 when cancelled.
 */
export function editManySessions(folder, tree) {
  const folderId = folder ? folder.id : "";
  const scopeName = folder ? `Ordner „${folder.name}“` : "Alle Sessions";
  const rows = [];
  // A row: checkbox "change this" + the input; typing ticks the box.
  const row = (label, input, note = "", watch = true) => {
    const on = h("input", { type: "checkbox" });
    const tick = () => { on.checked = true; update(); };
    if (watch) {
      input.addEventListener("input", tick);
      input.addEventListener("change", tick);
    }
    on.addEventListener("change", update);
    rows.push(on);
    return { on, el: h("div", { class: "bulk-row" }, h("label", { class: "check" }, on, h("span", {}, label, note ? h("span", { class: "muted" }, " · " + note) : null)), input) };
  };

  const user = h("input", { placeholder: "z. B. admin", spellcheck: "false" });
  const password = h("input", { type: "password", placeholder: "leer lassen = gespeicherte Passwörter entfernen" });
  const auth = h("select", {},
    h("option", { value: "auto" }, "Automatisch (Agent, Schlüssel, Passwort)"),
    h("option", { value: "password" }, "Passwort"),
    h("option", { value: "key" }, "Schlüsseldatei"),
    h("option", { value: "agent" }, "SSH-Agent"));
  const keyPath = h("input", { placeholder: "~/.ssh/id_ed25519", spellcheck: "false" });
  const keepAlive = h("input", { type: "number", min: 0, value: 30 });
  let color = "";
  const swatches = h("div", { class: "swatches" }, COLORS.map((c) => h("span", {
    class: "swatch" + (c ? "" : " none"), style: c ? { background: c } : {}, title: c || "keine Farbe",
    onclick: (e) => {
      color = c;
      swatches.querySelectorAll(".swatch").forEach((x) => x.classList.remove("on"));
      e.currentTarget.classList.add("on");
      colorRow.on.checked = true;
      update();
    },
  })));
  const userRow = row("Benutzer", user, "SSH, Seriell, RDP · Lines wie :6 bleiben");
  const pwRow = row("Passwort", password, "verschlüsselt gespeichert");
  const authRow = row("Anmeldung", auth, "nur SSH");
  const keyRow = row("Schlüsseldatei", keyPath, "nur SSH");
  const kaRow = row("Keepalive (Sekunden)", keepAlive, "nur SSH");
  const colorRow = row("Farbe", swatches, "", false); // a swatch click ticks the row itself
  const recursive = h("input", { type: "checkbox", checked: true });
  const count = h("p", { class: "hint" });

  function update() {
    const list = inScope(tree, folderId, recursive.checked);
    const by = (p) => list.filter((s) => (s.protocol || "ssh") === p).length;
    const parts = [["SSH", by("ssh")], ["Seriell", by("serial")], ["RDP", by("rdp")]].filter(([, n]) => n).map(([l, n]) => `${n} ${l}`);
    count.textContent = `Betrifft ${list.length} Session${list.length === 1 ? "" : "s"}${parts.length ? ` (${parts.join(", ")})` : ""}. Nur angehakte Felder werden geändert.`;
    okBtn.disabled = !list.length || !rows.some((r) => r.checked);
  }
  recursive.addEventListener("change", update);
  const okBtn = h("button", { class: "btn primary" }, "Übernehmen");

  return modal((close) => {
    okBtn.onclick = async () => {
      const p = {};
      if (userRow.on.checked) p.user = user.value.trim();
      if (pwRow.on.checked) p.password = password.value;
      if (authRow.on.checked) p.auth = auth.value;
      if (keyRow.on.checked) p.keyPath = keyPath.value.trim();
      if (kaRow.on.checked) p.keepAlive = Math.max(0, parseInt(keepAlive.value, 10) || 0);
      if (colorRow.on.checked) p.color = color;
      try {
        const n = await api.PatchSessions(folderId, recursive.checked, p);
        toast(`${n} Session${n === 1 ? "" : "s"} geändert.`, "ok");
        close(n);
      } catch (e) {
        toast(errMsg(e), "err");
      }
    };
    setTimeout(update, 0);
    return {
      head: `Für alle Sessions ändern – ${scopeName}`,
      wide: true,
      body: h("div", { class: "bulk" },
        userRow.el, pwRow.el, authRow.el, keyRow.el, kaRow.el, colorRow.el,
        h("label", { class: "check" }, recursive, folder ? "Unterordner einbeziehen" : "Auch Sessions in Ordnern (sonst nur oberste Ebene)"),
        count),
      foot: [h("button", { class: "btn", onclick: () => close(0) }, "Abbrechen"), okBtn],
    };
  }).then((n) => n || 0);
}
