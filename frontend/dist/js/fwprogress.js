// Small cards at the bottom right that show firmware downloads while they run
// (device, file, progress, speed, time left) – no need to keep the firmware
// dialog open. When done, "verify /md5" can be typed into the active tab.
import { api, errMsg } from "./api.js";
import { closeX, fmtSize, h, toast } from "./ui.js";

const KEEP_DONE_MS = 60000;
const KEEP_ERROR_MS = 30000;

let box = null;
const cards = new Map(); // key -> {el, parts, start, lastDone, lastTime, speed, timer}

function container() {
  if (!box) {
    box = h("div", { class: "fwp-box" });
    document.body.append(box);
  }
  return box;
}

const keyOf = (e) => `${e.proto}|${e.client}|${e.file}`;

function fmtTime(sec) {
  if (!isFinite(sec) || sec < 0) return "";
  sec = Math.round(sec);
  return sec >= 60 ? `${Math.floor(sec / 60)} min ${sec % 60} s` : `${sec} s`;
}

function remove(key) {
  const c = cards.get(key);
  if (!c) return;
  clearTimeout(c.timer);
  c.el.remove();
  cards.delete(key);
}

function card(e) {
  const key = keyOf(e);
  let c = cards.get(key);
  if (c) return c;
  const parts = {
    title: h("b", {}),
    sub: h("div", { class: "fwp-sub" }),
    bar: h("i", {}),
    line: h("div", { class: "fwp-line" }),
    actions: h("div", { class: "fwp-actions" }),
  };
  const el = h("div", { class: "fwp" },
    h("div", { class: "fwp-head" }, parts.title, closeX("Ausblenden", () => remove(key))),
    parts.sub, h("div", { class: "bar" }, parts.bar), parts.line, parts.actions);
  c = { el, parts, start: Date.now(), lastDone: 0, lastTime: Date.now(), speed: 0, timer: 0 };
  cards.set(key, c);
  container().append(el);
  return c;
}

/** Feed every firmware server event here. */
export function fwProgress(e, currentTab) {
  if (e.proto === "Server" || !e.file) return;
  const key = keyOf(e);
  if (e.state === "info" || e.state === "abgelehnt") { remove(key); return; }
  const c = card(e);
  const base = e.file.split(/[\\/]/).pop();
  const pct = e.total ? Math.min(100, Math.round((e.done / e.total) * 100)) : 0;
  c.parts.title.textContent = e.upload ? `Config ← ${e.client}` : `Firmware → ${e.client}`;
  c.parts.sub.textContent = `${base} · ${e.proto}`;
  c.parts.bar.style.width = (e.state === "fertig" ? 100 : pct) + "%";
  c.el.classList.toggle("done", e.state === "fertig");
  c.el.classList.toggle("bad", e.state === "fehler");
  clearTimeout(c.timer);

  if (e.state === "läuft") {
    const now = Date.now();
    const dt = (now - c.lastTime) / 1000;
    if (dt > 0.2 && e.done > c.lastDone) {
      const rate = (e.done - c.lastDone) / dt;
      c.speed = c.speed ? c.speed * 0.6 + rate * 0.4 : rate;
    }
    c.lastDone = e.done;
    c.lastTime = now;
    const left = c.speed > 0 && e.total ? (e.total - e.done) / c.speed : NaN;
    c.parts.line.textContent = e.done
      ? `${fmtSize(e.done)} von ${fmtSize(e.total)} (${pct} %)${c.speed ? ` · ${fmtSize(c.speed)}/s` : ""}${isFinite(left) ? ` · noch ${fmtTime(left)}` : ""}`
      : `Start … ${fmtSize(e.total)}`;
    c.parts.actions.replaceChildren();
    c.timer = setTimeout(() => remove(key), KEEP_ERROR_MS * 4); // no news for 2 min: the transfer is gone
    return;
  }
  if (e.state === "fertig") {
    const took = (Date.now() - c.start) / 1000;
    c.parts.line.textContent = e.upload ? `${e.detail || "gespeichert"} · ${fmtSize(e.done)}` : `fertig · ${fmtSize(e.done)} in ${fmtTime(took)}`;
    if (e.upload) {
      c.parts.actions.replaceChildren(h("button", { class: "btn small", onclick: () => api.OpenConfigDir().catch((err) => toast(errMsg(err), "err")) }, "Ordner öffnen"));
      c.timer = setTimeout(() => remove(key), KEEP_DONE_MS);
      return;
    }
    const verify = e.md5 ? `verify /md5 flash:${base} ${e.md5}` : "";
    c.parts.actions.replaceChildren(...(verify ? [h("button", {
      class: "btn small", title: verify,
      onclick: () => {
        const tab = currentTab();
        if (!tab || tab.state !== "connected") { toast("Erst die Switch-Sitzung öffnen."); return; }
        api.Write(tab.id, verify).then(() => tab.focus()).catch((err) => toast(errMsg(err), "err"));
      },
    }, "verify /md5 einfügen")] : []));
    c.timer = setTimeout(() => remove(key), KEEP_DONE_MS);
    return;
  }
  c.parts.line.textContent = `${e.state}${e.detail ? ": " + e.detail : ""}`;
  c.parts.actions.replaceChildren();
  c.timer = setTimeout(() => remove(key), KEEP_ERROR_MS);
}
