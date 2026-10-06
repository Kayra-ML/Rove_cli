export type SlashId =
  | "new" | "undo" | "redo" | "stop" | "clear" | "help"
  | "map" | "context"
  | "goal" | "rename" | "export" | "remember" | "checkpoint" | "restore" | "usage" | "models" | "session" | "diff" | "compact" | "panes" | "to" | "all";

export type SlashCmd = {
  id: SlashId;
  aliases: string[];
  hint: string;
  // arg shows as a placeholder after the command in the menu
  arg?: string;
  // where the command applies: the terminal mode only, or the chat only
  // (Orchestra, Office); unset: everywhere
  only?: SlashScope;
};

export type SlashScope = "terminal" | "chat";

export const SLASH: SlashCmd[] = [
  // roles are picked per terminal; in chats the agent comes from the sidebar
  { id: "panes", aliases: ["panes", "terminals", "terminaller", "bolmeler", "bölmeler"], hint: "Bu oturumun terminalleri: kim ne yapıyor", only: "terminal" },
  { id: "to", aliases: ["to", "gönder", "gonder", "send"], hint: "Başka bir terminale iş ver (/to 2 testleri yaz, ya da @T2 …)", arg: "<T2|rol> <mesaj>", only: "terminal" },
  { id: "all", aliases: ["all", "hepsi", "herkese"], hint: "Diğer bütün terminallere aynı işi ver", arg: "<mesaj>", only: "terminal" },
  { id: "models", aliases: ["models", "model", "modeller"], hint: "Bu sohbetin modelini seç", arg: "[model]" },
  { id: "goal", aliases: ["goal", "hedef"], hint: "Hedef koy; ajan kriterler tutana kadar çalışır", arg: "<hedef> | kriter, kriter" },
  { id: "map", aliases: ["map", "harita"], hint: "Koddan bağlam ekle / haritayı aç", arg: "[arama]" },
  { id: "context", aliases: ["context", "bağlam", "baglam"], hint: "Bağlam haritasını aç" },
  { id: "new", aliases: ["new", "reset", "yeni"], hint: "Yeni sohbet" },
  { id: "diff", aliases: ["diff", "changes", "değişiklikler", "degisiklikler"], hint: "Son turun değişikliklerini incele, kabul et ya da geri al" },
  { id: "compact", aliases: ["compact", "özetle", "ozetle"], hint: "Uzun sohbetin eski kısmını özetle (token tasarrufu)" },
  { id: "session", aliases: ["session", "sessions", "oturum", "oturumlar", "sohbetler"], hint: "Eski oturumlara geç", arg: "[ara]" },
  { id: "rename", aliases: ["rename", "adlandır", "adlandir", "başlık", "baslik"], hint: "Sohbetin adını değiştir", arg: "<yeni ad>" },
  { id: "remember", aliases: ["remember", "hatırla", "hatirla", "not"], hint: "Projeye kalıcı not ekle", arg: "<not>" },
  { id: "checkpoint", aliases: ["checkpoint", "snapshot", "kaydet"], hint: "Dosyaların anlık görüntüsünü al", arg: "[etiket]" },
  { id: "restore", aliases: ["restore", "geriyükle", "geriyukle"], hint: "Son anlık görüntüye dön" },
  { id: "undo", aliases: ["undo", "geri"], hint: "Son turu geri al" },
  { id: "redo", aliases: ["redo"], hint: "Geri alınan turu geri getir" },
  { id: "stop", aliases: ["stop", "cancel", "dur"], hint: "Çalışan ajanı durdur" },
  { id: "clear", aliases: ["clear", "temizle"], hint: "Bu sohbetin geçmişini sil" },
  { id: "export", aliases: ["export", "indir"], hint: "Sohbeti .md olarak indir" },
  { id: "usage", aliases: ["usage", "kullanım", "kullanim", "cost", "maliyet"], hint: "Token kullanımını göster" },
  { id: "help", aliases: ["help", "yardım", "yardim"], hint: "Komut listesi" },
];

// parseGoal reads "/goal <title> | criterion, criterion". Without criteria
// the title itself is what the judge checks.
export function parseGoal(rest: string): { title: string; criteria: string[] } | null {
  const [title, crit = ""] = rest.split("|").map((x) => x.trim());
  if (!title) return null;
  const criteria = crit.split(",").map((x) => x.trim()).filter(Boolean);
  return { title, criteria: criteria.length ? criteria : [title] };
}

function norm(s: string): string {
  return s.toLocaleLowerCase("tr");
}

const inScope = (c: SlashCmd, scope?: SlashScope) => !scope || !c.only || c.only === scope;

export function matchSlash(raw: string, scope?: SlashScope): { cmd: SlashCmd; rest: string } | null {
  const t = raw.trim();
  if (!t.startsWith("/")) return null;
  const body = t.slice(1);
  const sp = body.search(/\s/);
  const name = norm(sp === -1 ? body : body.slice(0, sp));
  const rest = sp === -1 ? "" : body.slice(sp + 1).trim();
  const cmd = SLASH.find((c) => c.aliases.includes(name) && inScope(c, scope));
  return cmd ? { cmd, rest } : null;
}

export function filterSlash(raw: string, scope?: SlashScope): SlashCmd[] {
  const t = raw.trim();
  if (!t.startsWith("/")) return [];
  // once an argument is being typed the menu gets out of the way
  if (/\s/.test(t)) return [];
  const q = norm(t.slice(1));
  const list = SLASH.filter((c) => inScope(c, scope));
  if (!q) return list;
  return list.filter((c) => {
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

export type ModelChoice = { provider: string; model: string; default?: boolean };

// filterModels keeps the models whose "provider/model" contains the query.
export function filterModels(list: ModelChoice[], query: string): ModelChoice[] {
  const q = norm(query.trim());
  if (!q) return list;
  return list.filter((m) => norm(`${m.provider}/${m.model}`).includes(q));
}

// pickModel resolves "/models <query>": an exact model (or provider/model)
// name, or the only model that contains the query; otherwise nothing.
export function pickModel(list: ModelChoice[], query: string): ModelChoice | null {
  const q = norm(query.trim());
  if (!q) return null;
  const exact = list.filter((m) => norm(m.model) === q || norm(`${m.provider}/${m.model}`) === q);
  if (exact.length === 1) return exact[0];
  const hits = filterModels(list, q);
  return hits.length === 1 ? hits[0] : null;
}

// filterSessions keeps the chats whose title contains the query.
export function filterSessions<T extends { title: string }>(list: T[], query: string): T[] {
  const q = norm(query.trim());
  if (!q) return list;
  return list.filter((s) => norm(s.title || "").includes(q));
}

// paneRef reads "@T2 message" / "@2 message": which terminal, and the message.
export function paneRef(text: string): { ref: string; message: string } | null {
  const m = /^@(t?\d+)\s+([\s\S]+)$/i.exec(text.trim());
  return m ? { ref: m[1], message: m[2].trim() } : null;
}
