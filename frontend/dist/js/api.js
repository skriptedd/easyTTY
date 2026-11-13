// Thin wrapper around the Wails bindings (window.go.main.App) and runtime.

const backend = () => window.go.main.App;
const rt = () => window.runtime;

export const api = new Proxy({}, {
  get: (_, name) => (...args) => backend()[name](...args),
});

export const events = {
  on: (name, cb) => rt().EventsOn(name, cb),
};

export const clipboard = {
  get: () => rt().ClipboardGetText(),
  set: (text) => rt().ClipboardSetText(text),
};

export const openURL = (url) => rt().BrowserOpenURL(url);

export function b64ToBytes(s) {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function errMsg(e) {
  if (!e) return "Unbekannter Fehler";
  if (typeof e === "string") return e;
  return e.message || String(e);
}
