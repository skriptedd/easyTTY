// Generic UI helpers: element builder, modals, context menus, toasts.

export function h(tag, attrs = {}, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "class") el.className = v;
    else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "value") el.value = v;
    else if (k === "checked") el.checked = !!v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

const SVG_NS = "http://www.w3.org/2000/svg";

// Line icons (24×24 grid, stroke = currentColor) – crisper than unicode glyphs.
const ICONS = {
  refresh: ["M20 11a8 8 0 1 0-2.3 5.7", "M20 4v7h-7"],
  sidebar: ["M4 5h16v14H4z", "M9 5v14"],
  tasks: ["M9 6h11", "M9 12h11", "M9 18h11", "M4 5.5l1 1 2-2", "M4 11.5l1 1 2-2", "M4 17.5l1 1 2-2"],
  firmware: ["M4 4h16v6H4z", "M4 14h16v6H4z", "M8 7h.01", "M8 17h.01", "M15 7h2", "M15 17h2"],
  monitor: ["M3 4h18v12H3z", "M8 20h8", "M12 16v4"],
};

/** <svg> icon that follows the text colour and size of its button. */
export function icon(name) {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("class", "ico-svg");
  svg.setAttribute("aria-hidden", "true");
  for (const d of ICONS[name]) {
    const p = document.createElementNS(SVG_NS, "path");
    p.setAttribute("d", d);
    svg.append(p);
  }
  return svg;
}

const layer = () => document.getElementById("layer");

// Native windows (RDP) would cover dialogs and menus; they hide while one is open.
let modals = 0;
let menuShown = false;
let dragActive = false;
export const overlayOpen = () => modals > 0 || menuShown || dragActive;
const overlayChanged = () => dispatchEvent(new Event("easytty:overlay"));

/** A native window would swallow an HTML drag, so it steps aside during one too. */
export function setDragActive(on) {
  if (on === dragActive) return;
  dragActive = on;
  overlayChanged();
}

/**
 * Opens a modal. build(close) returns {head, body, foot, wide, danger, onKey}.
 * Returns a promise resolved with the value passed to close().
 */
export function modal(build) {
  return new Promise((resolve) => {
    let done = false;
    const prevFocus = document.activeElement;
    const close = (value) => {
      if (done) return;
      done = true;
      back.remove();
      modals--;
      overlayChanged();
      document.removeEventListener("keydown", onKey, true);
      if (prevFocus && prevFocus.focus) prevFocus.focus();
      resolve(value);
    };
    const spec = build(close);
    const box = h("div", { class: "modal" + (spec.wide ? " wide" : "") },
      h("div", { class: "modal-head" + (spec.danger ? " danger" : "") },
        h("span", {}, spec.head), closeX("Schließen (Esc)", () => close(undefined))),
      h("div", { class: "modal-body" }, spec.body),
      spec.foot ? h("div", { class: "modal-foot" }, spec.foot) : null,
    );
    const back = h("div", { class: "backdrop" }, box);
    back.addEventListener("mousedown", (e) => { if (e.target === back && !spec.sticky) close(undefined); });
    const onKey = (e) => {
      if (back !== layer().lastElementChild) return; // only the top-most modal reacts
      if (e.key === "Escape" && e.target?.type === "search" && e.target.value) {
        // Esc in a filled search box clears it instead of throwing the dialog away.
        e.preventDefault(); e.stopPropagation();
        e.target.value = ""; e.target.dispatchEvent(new Event("input"));
      } else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(undefined); }
      else if (spec.onKey) spec.onKey(e);
    };
    document.addEventListener("keydown", onKey, true);
    layer().append(back);
    modals++;
    overlayChanged();
    const first = box.querySelector("[autofocus]") || box.querySelector("input, select, textarea, button.primary");
    if (first) setTimeout(() => first.focus(), 0);
  });
}

export function confirmDialog({ title, message, ok = "OK", danger = false }) {
  return modal((close) => ({
    head: title,
    danger,
    body: h("p", {}, message),
    foot: [
      h("button", { class: "btn", onclick: () => close(false) }, "Abbrechen"),
      h("button", { class: "btn primary" + (danger ? " danger" : ""), onclick: () => close(true) }, ok),
    ],
    onKey: (e) => {
      if (e.key !== "Enter" || (e.target.tagName === "BUTTON" && !e.target.classList.contains("primary"))) return;
      e.preventDefault();
      close(true);
    },
  })).then((v) => !!v);
}

export function inputDialog({ title, label, value = "", ok = "OK" }) {
  const input = h("input", { value, autofocus: true, spellcheck: "false" });
  return modal((close) => ({
    head: title,
    body: h("label", { class: "field" }, h("span", {}, label), input),
    foot: [
      h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
      h("button", { class: "btn primary", onclick: () => close(input.value) }, ok),
    ],
    onKey: (e) => { if (e.key === "Enter") { e.preventDefault(); close(input.value); } },
  })).then((v) => (v === undefined ? null : v));
}

let openMenu = null;
export function closeMenu() {
  if (openMenu) { openMenu.remove(); openMenu = null; }
  if (menuShown) { menuShown = false; overlayChanged(); }
}

/** items: [{label, action, hint, danger, disabled} | "-"]. Returns the menu element. */
export function contextMenu(x, y, items) {
  closeMenu();
  const menu = h("div", { class: "menu" },
    items.map((it) => it === "-" ? h("hr") : h("button", {
      class: it.danger ? "danger" : "",
      disabled: it.disabled,
      onclick: () => { closeMenu(); it.action(); },
    }, h("span", {}, it.label), it.hint ? h("span", { class: "k" }, it.hint) : null)),
  );
  document.body.append(menu);
  const r = menu.getBoundingClientRect();
  menu.style.left = Math.min(x, innerWidth - r.width - 4) + "px";
  menu.style.top = Math.min(y, innerHeight - r.height - 4) + "px";
  openMenu = menu;
  menuShown = true;
  overlayChanged();
  return menu;
}
addEventListener("mousedown", (e) => { if (openMenu && !openMenu.contains(e.target)) closeMenu(); }, true);
addEventListener("keydown", (e) => { if (e.key === "Escape") closeMenu(); }, true);
addEventListener("blur", closeMenu);

/** The small ✕ at the top right of floating bars, cards and messages. */
export function closeX(title, onclick) {
  return h("button", { class: "mini-x", type: "button", title, "aria-label": title, onclick }, "✕");
}

let toastBox = null;
/** actions: optional [{label, action}] shown as small buttons under the message. */
export function toast(message, kind = "info", ms = 3800, actions = []) {
  if (!toastBox) { toastBox = h("div", { class: "toasts" }); document.body.append(toastBox); }
  const t = h("div", { class: "toast " + kind }, h("span", {}, message),
    actions.length ? h("div", { class: "toast-actions" }, actions.map((a) => h("button", { class: "btn small", type: "button", onclick: a.action }, a.label))) : null,
    closeX("Meldung schließen", () => t.remove()));
  toastBox.append(t);
  setTimeout(() => t.remove(), ms);
}

/** Makes a vertical splitter resize `target` horizontally. sign=-1 if target is right of the splitter. */
export function makeSplitter(splitter, target, { sign = 1, min = 160, max = 900, onEnd, storageKey } = {}) {
  if (storageKey) {
    try { const w = localStorage.getItem(storageKey); if (w) target.style.width = w + "px"; } catch { /* storage unavailable */ }
  }
  splitter.addEventListener("mousedown", (e) => {
    e.preventDefault();
    const startX = e.clientX;
    const startW = target.getBoundingClientRect().width;
    splitter.classList.add("drag");
    const move = (ev) => {
      const w = Math.max(min, Math.min(max, startW + sign * (ev.clientX - startX)));
      target.style.width = w + "px";
    };
    const up = () => {
      splitter.classList.remove("drag");
      removeEventListener("mousemove", move);
      removeEventListener("mouseup", up);
      if (storageKey) {
        try { localStorage.setItem(storageKey, String(Math.round(target.getBoundingClientRect().width))); } catch { /* ignore */ }
      }
      if (onEnd) onEnd();
    };
    addEventListener("mousemove", move);
    addEventListener("mouseup", up);
  });
}

export function fmtSize(n) {
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n < 10 ? 1 : 0) + " " + u[i];
}

export function fmtDate(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  const p = (x) => String(x).padStart(2, "0");
  return `${p(d.getDate())}.${p(d.getMonth() + 1)}.${String(d.getFullYear()).slice(2)} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export const store = {
  get(key, fallback) {
    try { const v = localStorage.getItem(key); return v === null ? fallback : JSON.parse(v); } catch { return fallback; }
  },
  set(key, value) {
    try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* ignore */ }
  },
};
