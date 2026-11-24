// How a session is shown in lists: where it connects to, in one short line.

export const SHELLS = [["powershell", "PowerShell"], ["pwsh", "PowerShell 7"], ["cmd", "Eingabeaufforderung"]];

export const shellName = (shell) => (SHELLS.find(([v]) => v === shell) || SHELLS[0])[1];

/** "admin@10.0.0.1", "COM3", "PowerShell lokal"; withPort adds a non-default port. */
export function sessionHow(s, { withPort = false } = {}) {
  if (s.protocol === "serial") return s.serialPort;
  if (s.protocol === "local") return shellName(s.shell) + " lokal";
  const defPort = s.protocol === "rdp" ? 3389 : 22;
  return (s.user ? s.user + "@" : "") + s.host + (withPort && s.port && s.port !== defPort ? ":" + s.port : "");
}
