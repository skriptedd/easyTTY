// Updates: a quiet note in the status bar when a newer version exists, and
// the dialog that downloads and installs it.
import { api, errMsg, events, openURL } from "./api.js";
import { h, modal, toast } from "./ui.js";

/**
 * Shows the update dialog. openCount: connections that a restart would close.
 * Resolves when the dialog closes; on success the app restarts instead.
 */
export function showUpdate(info, openCount = 0) {
  let stop = () => {};
  return modal((close) => {
    const bar = h("div", { class: "update-bar" }, h("span", {}));
    const state = h("p", { class: "hint" }, openCount
      ? `easyTTY startet danach neu. ${openCount} offene Verbindung${openCount === 1 ? " wird" : "en werden"} dabei getrennt.`
      : "easyTTY startet danach neu.");
    const later = h("button", { class: "btn", onclick: () => close(null) }, "Später");
    const install = h("button", { class: "btn primary" }, "Jetzt aktualisieren");
    stop = events.on("update:progress", (pct) => {
      bar.firstChild.style.width = pct + "%";
      state.textContent = `Lade Version ${info.version}… ${pct} %`;
    });
    install.onclick = async () => {
      install.disabled = later.disabled = true;
      bar.classList.add("on");
      state.textContent = `Lade Version ${info.version}…`;
      try {
        await api.InstallUpdate(info.version);
        state.textContent = "Geprüft und installiert. easyTTY startet neu…";
      } catch (e) {
        bar.classList.remove("on");
        state.textContent = "Nichts wurde geändert.";
        install.disabled = later.disabled = false;
        toast(errMsg(e), "err", 8000);
      }
    };
    return {
      head: `Update auf easyTTY ${info.version}`,
      sticky: true,
      body: h("div", {},
        h("p", {}, `Installiert ist ${info.current}.`),
        info.notes ? h("pre", { class: "update-notes" }, info.notes) : null,
        bar, state),
      foot: [
        info.page ? h("button", { class: "btn left", onclick: () => openURL(info.page) }, "Release-Seite") : null,
        later, install,
      ],
    };
  }).then(() => { if (typeof stop === "function") stop(); });
}

/** Listens for the start-up check and offers the update in the status bar. */
export function initUpdates(slot, openCount) {
  events.on("update:available", (info) => {
    slot.replaceChildren(h("button", {
      class: "status-help update-note", title: `Installiert ist ${info.current}`,
      onclick: () => showUpdate(info, openCount()),
    }, `Update ${info.version} verfügbar`));
  });
}

/** "Nach Updates suchen" in the settings: always answers with a message or the dialog. */
export async function checkNow(openCount) {
  try {
    const info = await api.CheckUpdate();
    if (info.available) return showUpdate(info, openCount());
    toast(info.version ? `easyTTY ${info.current} ist aktuell.` : "Keine veröffentlichte Version gefunden.", "ok");
  } catch (e) {
    toast(errMsg(e), "err");
  }
}

/** "v0.2.15" in the status bar (click: look for updates) and on the start screen. */
export async function showVersion(statusSlot, welcomeSlot, openCount) {
  const info = await api.AppInfo().catch(() => null);
  if (!info) return;
  const dev = !info.version || info.version.endsWith("-dev");
  const label = dev ? `Dev-Build ${info.version || ""}`.trim() : `v${info.version}`;
  statusSlot.replaceChildren(h("button", {
    class: "status-help", title: `easyTTY ${info.version} (${info.mode}) · Klick: nach Updates suchen`,
    onclick: () => checkNow(openCount),
  }, label));
  welcomeSlot.textContent = dev ? label : `Version ${info.version}`;
}
