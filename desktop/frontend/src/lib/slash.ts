export type SlashId = "new" | "undo" | "redo" | "review" | "stop" | "clear" | "help" | "run";

export type SlashCmd = {
  id: SlashId;
  aliases: string[];
  hint: string;
};

export const SLASH: SlashCmd[] = [
  { id: "new", aliases: ["new", "reset"], hint: "Yeni sohbet" },
  { id: "undo", aliases: ["undo"], hint: "Son turu geri al" },
  { id: "redo", aliases: ["redo"], hint: "Geri alınan turu geri getir" },
  { id: "review", aliases: ["review"], hint: "Son yanıtı review kartına al" },
  { id: "stop", aliases: ["stop", "cancel"], hint: "Çalışan ajanı durdur" },
  { id: "run", aliases: ["run", "sweep"], hint: "Ready kartları şimdi koştur" },
  { id: "clear", aliases: ["clear"], hint: "Bu sohbetin geçmişini sil" },
  { id: "help", aliases: ["help"], hint: "Komut listesi" },
];

export function matchSlash(raw: string): { cmd: SlashCmd; rest: string } | null {
  const t = raw.trim();
  if (!t.startsWith("/")) return null;
  const body = t.slice(1);
  const sp = body.search(/\s/);
  const name = (sp === -1 ? body : body.slice(0, sp)).toLowerCase();
  const rest = sp === -1 ? "" : body.slice(sp + 1).trim();
  const cmd = SLASH.find((c) => c.aliases.includes(name));
  return cmd ? { cmd, rest } : null;
}

export function filterSlash(raw: string): SlashCmd[] {
  const t = raw.trim();
  if (!t.startsWith("/")) return [];
  const q = t.slice(1).split(/\s/)[0].toLowerCase();
  if (!q) return SLASH;
  return SLASH.filter((c) => {
    if (c.id.startsWith(q)) return true;
    return c.aliases.some((a) => a !== c.id && a.startsWith(q) && q.length >= 3);
  });
}

export function lastUserKeep(messages: { role: string }[]): number {
  let last = -1;
  for (let i = 0; i < messages.length; i++) {
    if (messages[i].role === "user") last = i;
  }
  return last < 0 ? messages.length : last;
}
