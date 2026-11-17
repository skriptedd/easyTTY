// "SSH-Verbindung": what an SSH tab negotiated and which keys it used.
import { api, clipboard, errMsg } from "./api.js";
import { h, modal, toast } from "./ui.js";

const AUTH = {
  publickey: "Schlüssel",
  password: "Passwort",
  "keyboard-interactive": "Passwort (Tastatur-Abfrage)",
};

const row = (label, ...value) => h("div", { class: "report-row" }, h("span", { class: "muted" }, label), h("span", {}, ...value));

/** Algorithm name, marked when easyTTY only allows it for old devices. */
const algo = (a, empty = "–") => (a?.name
  ? [a.name, a.legacy ? h("span", { class: "legacy-mark", title: "Gilt heute als schwach; easyTTY lässt es nur für ältere Geräte zu" }, " veraltet") : null]
  : [empty]);

const keyText = (k) => [k.type, k.bits ? `${k.bits} Bit` : ""].filter(Boolean).join(", ");

function fingerprint(fp) {
  if (!fp) return ["–"];
  return [fp, " ", h("button", { class: "btn ghost small", type: "button", title: "Fingerabdruck kopieren",
    onclick: () => { clipboard.set(fp); toast("Fingerabdruck kopiert"); } }, "⧉")];
}

function body(info) {
  const aead = "in der Verschlüsselung enthalten";
  const auth = AUTH[info.auth] || info.auth || "–";
  const legacy = [info.keyExchange, info.hostKeyAlgo, info.cipherTo, info.cipherFrom, info.macTo, info.macFrom].some((a) => a?.legacy);
  return h("div", { class: "report" },
    h("div", { class: "report-head" }, "Verbindung"),
    row("Ziel", `${info.user}@${info.addr}`),
    info.jump?.length ? row("Über", info.jump.join(" → ")) : null,
    row("Server", info.serverVersion || "–"),
    row("easyTTY", info.clientVersion || "–"),
    h("div", { class: "report-head" }, "Verfahren"),
    row("Schlüsseltausch", ...algo(info.keyExchange)),
    row("Hostkey", ...algo(info.hostKeyAlgo)),
    row("Verschlüsselung →", ...algo(info.cipherTo)),
    row("Verschlüsselung ←", ...algo(info.cipherFrom)),
    row("MAC →", ...algo(info.macTo, aead)),
    row("MAC ←", ...algo(info.macFrom, aead)),
    legacy ? h("p", { class: "hint" }, "„veraltet“: Das Gerät kann nichts Neueres. easyTTY erlaubt es, damit alte Switches erreichbar bleiben. Wenn möglich auf dem Gerät modernere Verfahren einschalten.") : null,
    h("div", { class: "report-head" }, "Schlüssel"),
    row("Hostkey des Servers", keyText(info.hostKey) || "–"),
    row("Fingerabdruck", ...fingerprint(info.hostKey?.fingerprint)),
    row("Anmeldung", auth),
    info.clientKey ? row("Eigener Schlüssel", `${info.clientKey.path} (${keyText(info.clientKey)})`) : null,
    info.clientKey ? row("Fingerabdruck", ...fingerprint(info.clientKey.fingerprint)) : null,
    info.auth === "publickey" && !info.clientKey ? row("Eigener Schlüssel", "aus dem SSH-Agenten oder ~/.ssh (id_ed25519, id_ecdsa, id_rsa)") : null,
    h("p", { class: "hint" }, "→ von easyTTY zum Server, ← vom Server zu easyTTY. Den Fingerabdruck kannst du mit dem Gerät vergleichen, z. B. „show crypto key mypubkey rsa“ oder „ssh-keygen -lf“ auf dem Server."));
}

export async function showSSHInfo(tabId, title) {
  let info;
  try { info = await api.SSHInfo(tabId); } catch (e) { toast(errMsg(e), "err"); return; }
  return modal((close) => ({
    head: `SSH-Verbindung – ${title}`,
    body: body(info),
    foot: [h("button", { class: "btn primary", onclick: () => close() }, "Schließen")],
  }));
}
