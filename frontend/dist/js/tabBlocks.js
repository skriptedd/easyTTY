// The tab bar shows one block per open layout instead of one tab per session.
// Plain tabs stay single entries; the member tabs live hidden inside the block.
import { confirmDialog, contextMenu, h } from "./ui.js";

const members = (mgr, g) => mgr.tabs.filter((t) => t.groupId === g.id);

/** Entries of the bar in order: {tab} or {group}, a layout where its first tab sits. */
export function barEntries(mgr) {
  const seen = new Set();
  const out = [];
  for (const tab of mgr.tabs) {
    const group = mgr.groupOf(tab);
    if (!group) out.push({ tab });
    else if (!seen.has(group)) { seen.add(group); out.push({ group }); }
  }
  return out;
}

/** The tab a block shows when switched to: the one last used there. */
export function blockTab(mgr, g) {
  const list = members(mgr, g);
  return list.find((t) => t.id === g.last) || list[0] || null;
}

async function closeBlock(mgr, g) {
  const list = members(mgr, g);
  const ok = await confirmDialog({
    title: "Layout schließen", ok: "Alle schließen", danger: true,
    message: `„${blockName(mgr, g)}“ schließt ${list.length === 1 ? "1 Session" : `${list.length} Sessions`}.`,
  });
  if (ok) list.forEach((t) => mgr.close(t.id));
}

function blockMenu(mgr, g, x, y) {
  contextMenu(x, y, [
    ...members(mgr, g).map((t) => ({ label: (t.id === mgr.active ? "▸ " : "") + t.title, action: () => mgr.activate(t.id) })),
    "-",
    { label: "In einzelne Tabs auflösen", action: () => mgr.dissolve(g.id) },
    { label: "Layout schließen", danger: true, action: () => closeBlock(mgr, g) },
  ]);
}

/** Folder layouts carry the folder's name, self-made views their first session. */
function blockName(mgr, g) {
  if (!g.adhoc) return g.name;
  const list = members(mgr, g);
  return list.length > 1 ? `${list[0].title} +${list.length - 1}` : list[0]?.title || g.name;
}

function makeBlock(mgr, g) {
  g.nameEl = h("span", { class: "title" });
  g.dotsEl = h("span", { class: "block-dots" });
  g.membersEl = h("span", { class: "block-members", hidden: true });
  g.blockEl = h("div", { class: "tab tab-block", draggable: "true" },
    h("span", { class: "group-mark" }, "▦"), g.nameEl, g.dotsEl,
    h("button", { class: "close", title: "Layout schließen", onclick: (e) => { e.stopPropagation(); closeBlock(mgr, g); } }, "×"),
    g.membersEl);
  g.blockEl.addEventListener("click", () => mgr.showGroup(g));
  g.blockEl.addEventListener("mousedown", (e) => { if (e.button === 1) { e.preventDefault(); closeBlock(mgr, g); } });
  g.blockEl.addEventListener("contextmenu", (e) => { e.preventDefault(); blockMenu(mgr, g, e.clientX, e.clientY); });
}

function paintBlock(mgr, g) {
  const list = members(mgr, g);
  g.nameEl.textContent = blockName(mgr, g);
  g.blockEl.title = list.map((t) => t.title).join("\n");
  g.blockEl.classList.toggle("active", mgr.groupOf(mgr.current()) === g);
  g.blockEl.classList.toggle("ai-active", list.some((t) => t.tabEl.classList.contains("ai-active")));
  g.dotsEl.replaceChildren(...list.map((t) => h("span", { class: t.tabEl.querySelector(".state").className })));
}

/** Brings the bar in line with the tabs and layouts; returns the entries. */
export function syncBlocks(mgr) {
  const entries = barEntries(mgr);
  if (mgr.reordering) return entries;
  const want = entries.map(({ tab, group }) => {
    if (tab) return tab.tabEl;
    if (!group.blockEl) makeBlock(mgr, group);
    return group.blockEl;
  });
  for (const { group } of entries) if (group) group.membersEl.replaceChildren(...members(mgr, group).map((t) => t.tabEl));
  const bar = mgr.tabbarEl;
  if (want.length !== bar.children.length || want.some((el, i) => bar.children[i] !== el)) bar.replaceChildren(...want);
  for (const { group } of entries) if (group) paintBlock(mgr, group);
  return entries;
}
