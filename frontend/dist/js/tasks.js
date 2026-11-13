// Tasks: saved command lists for one or more sessions, run by hand or on a
// schedule (once, daily, weekly, interval). Every run keeps its output.
import { api, clipboard, errMsg, events } from "./api.js";
import { confirmDialog, fmtDate, h, modal, toast } from "./ui.js";

const KINDS = [["manual", "Von Hand"], ["once", "Einmal"], ["daily", "Täglich"], ["weekly", "Wöchentlich"], ["interval", "Intervall"]];
const DEVICES = [["auto", "Automatisch erkennen"], ["cisco", "Switch / Router (terminal length 0)"], ["other", "Linux / anderes (nichts extra)"]];
const DAYS = ["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"];
const STATE_LABEL = { "läuft": "läuft …", ok: "OK", fehler: "Fehler", abgebrochen: "abgebrochen", verpasst: "verpasst" };

let onRun = null; // set while the task list is open
let currentSession = () => null; // session of the active tab (preselected in a new task)

/**
 * Call once at startup: toasts for finished runs while the list is closed.
 * getCurrentSession returns the session of the active tab (or null).
 */
export function initTasks(getCurrentSession) {
  if (getCurrentSession) currentSession = getCurrentSession;
  events.on("task:run", (r) => {
    if (onRun) onRun(r);
    else if (r.state === "fehler" || r.state === "verpasst") toast(`Aufgabe „${r.taskName}“: ${STATE_LABEL[r.state]}${r.error ? " – " + r.error : ""}`, "err", 9000);
    else if (r.state === "ok") toast(`Aufgabe „${r.taskName}“ fertig.`, "ok");
  });
}

function field(label, input, cls = "") {
  return h("label", { class: "field " + cls }, h("span", {}, label), input);
}

const badge = (state) => h("span", { class: "task-state " + (state || "none") }, STATE_LABEL[state] || "noch nie");

function relTime(ms) {
  if (!ms) return "";
  const d = ms - Date.now();
  const abs = Math.abs(d), min = Math.round(abs / 60000);
  const txt = min < 60 ? `${min} min` : min < 48 * 60 ? `${Math.round(min / 60)} h` : `${Math.round(min / 1440)} Tagen`;
  return d >= 0 ? `in ${txt}` : `vor ${txt}`;
}

/** Session picker grouped by folder, with a search box; ticks survive filtering. */
function sessionPicker(tree, selected) {
  // Only sessions the list shows (deleted or RDP ones would stay ticked invisibly).
  const listed = new Set(tree.sessions.filter((x) => x.protocol !== "rdp").map((x) => x.id));
  const chosen = new Set(selected.filter((id) => listed.has(id)));
  const folders = new Map(tree.folders.map((f) => [f.id, f]));
  const path = (id) => { const out = []; for (let f = folders.get(id), g = 0; f && g < 32; f = folders.get(f.parentId), g++) out.unshift(f.name); return out.join(" › ") || "Oberste Ebene"; };
  const groups = new Map();
  for (const s of tree.sessions.filter((x) => x.protocol !== "rdp").sort((a, b) => a.name.localeCompare(b.name))) {
    const key = path(s.folderId);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(s);
  }
  const counter = h("span", { class: "muted task-count" });
  const count = () => { counter.textContent = chosen.size ? `${chosen.size} ausgewählt` : "keine ausgewählt"; };
  const rows = []; // {el, text, group}
  const groupEls = [...groups.entries()].sort().map(([folder, list]) => {
    const head = h("div", { class: "task-folder" }, folder);
    const items = list.map((s) => {
      const cb = h("input", { type: "checkbox", checked: chosen.has(s.id) });
      cb.addEventListener("change", () => { if (cb.checked) chosen.add(s.id); else chosen.delete(s.id); count(); });
      const how = s.protocol === "serial" ? s.serialPort : `${s.user ? s.user + "@" : ""}${s.host}`;
      const warn = s.protocol === "ssh" && (!s.user || (s.auth === "password" && !s.hasPassword)) ? " ⚠ ohne gespeicherte Anmeldung" : "";
      const el = h("label", { class: "check" }, cb, h("span", {}, s.name), h("span", { class: "muted" }, " " + how + warn));
      rows.push({ el, cb, text: [s.name, s.host, s.user, s.serialPort, folder].join(" ").toLowerCase() });
      return el;
    });
    const group = h("div", {}, head, items);
    group.rows = rows.slice(-items.length);
    return group;
  });
  const list = h("div", { class: "task-sessions" }, groupEls);
  const empty = h("div", { class: "muted task-empty" }, "Keine Session passt zur Suche.");
  list.append(empty);
  const search = h("input", { class: "task-search", type: "search", placeholder: "Sessions suchen (Name, Host, Benutzer, Ordner) …", spellcheck: "false" });
  const filter = () => {
    const words = search.value.toLowerCase().split(/\s+/).filter(Boolean);
    let shown = 0;
    for (const g of groupEls) {
      let any = false;
      for (const r of g.rows) {
        const hit = words.every((w) => r.text.includes(w));
        r.el.hidden = !hit;
        any ||= hit;
        if (hit) shown++;
      }
      g.hidden = !any;
    }
    empty.hidden = shown > 0;
  };
  search.addEventListener("input", filter);
  // Enter in the search ticks the only hit (quick: type, Enter).
  search.addEventListener("keydown", (e) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    e.stopPropagation();
    const hits = rows.filter((r) => !r.el.hidden);
    if (hits.length === 1) { hits[0].cb.checked = !hits[0].cb.checked; hits[0].cb.dispatchEvent(new Event("change")); }
  });
  filter();
  count();
  const el = h("div", { class: "task-picker" }, h("div", { class: "row" }, search, counter), list);
  // A preselected session is scrolled into view.
  setTimeout(() => rows.find((r) => r.cb.checked)?.el.scrollIntoView({ block: "nearest" }), 0);
  return { el, value: () => [...chosen] };
}

/** Opens the task editor. Resolves with the saved task (and run=true for "Speichern & ausführen") or null. */
export async function editTask(initial = {}) {
  const tree = await api.GetTree();
  // A new task starts on the session of the active tab (not for remote desktops).
  const cur = !initial.id && !initial.sessionIds ? currentSession() : null;
  const preset = cur && cur.protocol !== "rdp" ? [cur.id] : [];
  const t = { name: "", sessionIds: preset, commands: "", enabled: true, device: "auto", stopOnError: true, autoConfirm: true, idleTimeout: 0, schedule: { kind: "manual" }, ...initial };
  const sc = { kind: "manual", ...t.schedule };
  const name = h("input", { value: t.name, placeholder: "z. B. Config-Backup Rechenzentrum", autofocus: true });
  const picker = sessionPicker(tree, t.sessionIds);
  const commands = h("textarea", { class: "task-cmds", spellcheck: "false", placeholder: "show running-config\n! Kommentar – Leerzeilen und ! / # werden übersprungen\nconf t\n interface Gi1/0/1\n  description Uplink\nend\nwrite memory" }, t.commands);
  // Schedule kinds as visible buttons (a dropdown hid that times can be set at all).
  const kind = { value: sc.kind };
  const kindSeg = h("div", { class: "seg task-kinds" }, KINDS.map(([v, l]) => {
    const b = h("button", { type: "button", class: v === kind.value ? "on" : "" }, l);
    b.addEventListener("click", () => {
      kind.value = v;
      for (const x of kindSeg.children) x.classList.toggle("on", x === b);
      renderWhen();
    });
    return b;
  }));
  const date = h("input", { type: "date", value: sc.date || localDate() });
  const at = h("input", { type: "time", value: sc.at || "02:00" });
  const every = h("input", { type: "number", min: 5, max: 10080, value: sc.everyMin || 60 });
  const days = new Set(sc.days || [1, 2, 3, 4, 5]);
  const dayBtns = h("div", { class: "seg" }, DAYS.map((d, i) => {
    const b = h("button", { type: "button", class: days.has(i + 1) ? "on" : "" }, d);
    b.addEventListener("click", () => { if (days.has(i + 1)) days.delete(i + 1); else days.add(i + 1); b.classList.toggle("on"); });
    return b;
  }));
  const whenRow = h("div", { class: "row task-when full" });
  const txt = (s) => h("span", {}, s);
  let enabledEl = null;
  const renderWhen = () => {
    const k = kind.value;
    whenRow.replaceChildren(...(k === "once" ? [txt("am"), date, txt("um"), at, txt("Uhr")]
      : k === "daily" ? [txt("jeden Tag um"), at, txt("Uhr")]
      : k === "weekly" ? [dayBtns, txt("um"), at, txt("Uhr")]
      : k === "interval" ? [txt("alle"), every, txt("Minuten")]
      : [h("span", { class: "muted" }, "Läuft nur, wenn du in der Aufgabenliste „▶“ oder hier „Speichern & jetzt ausführen“ drückst.")]));
    if (enabledEl) enabledEl.style.display = k === "manual" ? "none" : "";
  };
  const device = h("select", {}, DEVICES.map(([v, l]) => h("option", { value: v, selected: v === t.device }, l)));
  const chk = (val, label) => { const c = h("input", { type: "checkbox", checked: !!val }); return [c, h("label", { class: "check full" }, c, label)]; };
  const [enabled, enabledLabel] = chk(t.enabled, "Zeitplan aktiv (ausschalten pausiert die Aufgabe)");
  enabledEl = enabledLabel;
  renderWhen();
  const [stop, stopEl] = chk(t.stopOnError, "Bei einem Fehler (z. B. „% Invalid input“) die restlichen Befehle auf diesem Gerät auslassen");
  const [confirm, confirmEl] = chk(t.autoConfirm, "Rückfragen wie „[confirm]“ oder „Destination filename […]?“ mit Enter bestätigen");
  const idle = h("input", { type: "number", min: 0, max: 600, value: t.idleTimeout || 30 });

  const collect = () => ({
    ...t, name: name.value, sessionIds: picker.value(), commands: commands.value, enabled: enabled.checked,
    device: device.value, stopOnError: stop.checked, autoConfirm: confirm.checked, idleTimeout: +idle.value || 0,
    schedule: { kind: kind.value, date: date.value, at: at.value, days: [...days].sort(), everyMin: +every.value || 0 },
  });
  return modal((close) => {
    const save = async (run) => {
      try {
        const saved = await api.SaveTask(collect());
        t.id = saved.id; // a retry after a failed run updates instead of creating a copy
        if (run) await api.RunTaskNow(saved.id);
        close({ task: saved, run });
      } catch (e) { toast(errMsg(e), "err", 7000); }
    };
    return {
      head: t.id ? "Aufgabe bearbeiten" : "Neue Aufgabe",
      wide: true,
      sticky: true,
      body: h("div", { class: "grid" },
        field("Name", name, "full"),
        field("Auf diesen Sessions (nacheinander)", picker.el, "full"),
        field("Befehle – eine Zeile pro Befehl, so wie du sie tippen würdest", commands, "full"),
        field("Wann", kindSeg, "full"), whenRow,
        enabledEl,
        field("Gerät", device), field("Max. Wartezeit ohne Ausgabe (s)", idle),
        stopEl, confirmEl,
        h("p", { class: "full hint" }, "„Jetzt ausführen“ tippt die Befehle sichtbar in den Tab der Session (offener Tab oder neuer). Zeitpläne laufen im Hintergrund ohne Rückfragen: Benutzer und Passwort (oder Schlüssel) in der Session speichern, Hostkey einmal bestätigen. Mitschrift im Session-Log. Zeitpläne laufen, solange easyTTY offen ist – ein verpasster Termin wird nicht nachgeholt.")),
      foot: [
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        h("button", { class: "btn", onclick: () => save(true) }, "Speichern & jetzt ausführen"),
        h("button", { class: "btn primary", onclick: () => save(false) }, "Speichern"),
      ],
    };
  });
}

/** Shows one run with the output of every command. */
export async function showRun(id) {
  let r;
  try { r = await api.TaskRun(id); } catch (e) { toast(errMsg(e), "err"); return; }
  const text = r.sessions.map((s) => `### ${s.session}\n` + (s.result.steps || []).map((st) => `${s.result.prompt || ">"} ${st.command}\n${st.output}${st.error ? "\n!! " + st.error : ""}`).join("\n") + (s.result.error ? `\n!! ${s.result.error}` : "")).join("\n\n");
  await modal((close) => ({
    head: `${r.taskName} – ${fmtDate(r.start)}`,
    wide: true,
    body: h("div", { class: "task-run" },
      h("div", { class: "task-run-head" }, badge(r.state), h("span", { class: "muted" }, `${r.trigger} · Dauer ${Math.max(0, Math.round((r.end - r.start) / 1000))} s`), r.error ? h("span", { class: "err" }, r.error) : null),
      (r.sessions || []).map((s) => h("details", { class: "task-dev", open: r.sessions.length <= 3 || !!s.result.error },
        h("summary", {}, badge(s.result.error || (s.result.steps || []).some((x) => x.error) ? "fehler" : "ok"), h("b", {}, s.session), s.result.prompt ? h("code", {}, s.result.prompt) : null,
          s.logPath ? h("button", { class: "btn small", onclick: (e) => { e.preventDefault(); api.RevealLog(s.logPath).catch((err) => toast(errMsg(err), "err")); } }, "Log") : null),
        s.result.error ? h("div", { class: "err" }, s.result.error) : null,
        (s.result.steps || []).map((st) => h("div", { class: "task-step" + (st.error ? " bad" : "") },
          h("div", { class: "task-cmd" }, h("code", {}, st.command), h("span", { class: "muted" }, `${st.ms} ms`)),
          st.output ? h("pre", {}, st.output) : null,
          st.error ? h("div", { class: "err" }, st.error) : null))))),
    foot: [
      h("button", { class: "btn", onclick: () => { clipboard.set(text); toast("Ausgabe kopiert"); } }, "Alles kopieren"),
      h("button", { class: "btn primary", onclick: () => close() }, "Schließen"),
    ],
  }));
}

async function showHistory(task) {
  let runs;
  try { runs = await api.TaskRuns(task?.id || "", 50); } catch (e) { toast(errMsg(e), "err"); return; }
  await modal((close) => ({
    head: task ? `Verlauf: ${task.name}` : "Verlauf aller Aufgaben",
    wide: true,
    body: runs.length ? h("div", { class: "task-list" }, runs.map((r) => h("div", { class: "task-row click", onclick: () => showRun(r.id) },
      badge(r.state), h("b", {}, r.taskName), h("span", { class: "muted" }, `${fmtDate(r.start)} · ${r.trigger}`),
      h("span", { class: "muted" }, r.error || `${r.sessions?.length || 0} Gerät(e)`)))) : h("div", { class: "sftp-msg" }, "Noch keine Läufe."),
    foot: [h("button", { class: "btn primary", onclick: () => close() }, "Schließen")],
  }));
}

/** Opens the task list. */
export async function openTasks() {
  const list = h("div", { class: "task-list" });
  const render = async () => {
    let rows;
    try { rows = await api.TaskOverview(); } catch (e) { list.replaceChildren(h("div", { class: "sftp-msg err" }, errMsg(e))); return; }
    if (!rows.length) {
      list.replaceChildren(h("div", { class: "sftp-msg" }, "Noch keine Aufgaben. Beispiele: jede Nacht „show running-config“ von allen Switchen sichern, am Samstag 22:00 eine Config-Änderung ausrollen, alle 15 Minuten „show interfaces status“ protokollieren."));
      return;
    }
    list.replaceChildren(...rows.map((v) => h("div", { class: "task-row" + (v.task.enabled || v.task.schedule.kind === "manual" ? "" : " off") },
      v.running ? badge("läuft") : badge(v.lastRun?.state),
      h("div", { class: "task-main" },
        h("b", {}, v.task.name,
          v.task.createdBy ? h("span", { class: "muted" }, ` · angelegt von ${v.task.createdBy}`) : null,
          v.task.modifiedBy ? h("span", { class: "task-ai" }, ` · geändert von ${v.task.modifiedBy}`) : null),
        h("div", { class: "muted" }, `${v.when}${v.next && v.task.enabled ? ` · nächster Lauf ${fmtDate(v.next)} (${relTime(v.next)})` : v.task.schedule.kind !== "manual" && !v.task.enabled ? " · pausiert" : ""}`),
        h("div", { class: "muted small" }, v.sessions.join(", "))),
      v.running
        ? h("button", { class: "btn", onclick: () => api.CancelTask(v.task.id).catch((e) => toast(errMsg(e), "err")) }, "Abbrechen")
        : h("button", { class: "btn", title: "Jetzt ausführen", onclick: () => api.RunTaskNow(v.task.id).then(render).catch((e) => toast(errMsg(e), "err")) }, "▶"),
      v.lastRun ? h("button", { class: "btn", title: "Letzten Lauf ansehen", onclick: () => showRun(v.lastRun.id) }, "Ergebnis") : h("span"),
      h("button", { class: "btn", title: "Verlauf", onclick: () => showHistory(v.task) }, "Verlauf"),
      h("button", { class: "btn", title: "Bearbeiten", onclick: async () => { if (await editTask(v.task)) render(); } }, "✎"),
      h("button", { class: "btn", title: "Löschen", onclick: async () => {
        if (await confirmDialog({ title: "Aufgabe löschen?", message: `„${v.task.name}“ wird gelöscht. Der Verlauf bleibt.`, ok: "Löschen", danger: true })) {
          api.DeleteTask(v.task.id).then(render).catch((e) => toast(errMsg(e), "err"));
        }
      } }, "🗑"))));
  };
  onRun = () => render();
  render();
  await modal((close) => ({
    head: "Aufgaben & Zeitpläne",
    wide: true,
    body: list,
    foot: [
      h("button", { class: "btn left", onclick: () => showHistory(null) }, "Verlauf aller Aufgaben"),
      h("button", { class: "btn", onclick: () => close() }, "Schließen"),
      h("button", { class: "btn primary", onclick: async () => { if (await editTask()) render(); } }, "+ Neue Aufgabe"),
    ],
  }));
  onRun = null;
}

/** Today as YYYY-MM-DD in local time (toISOString would give the UTC date). */
function localDate(d = new Date()) {
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}
