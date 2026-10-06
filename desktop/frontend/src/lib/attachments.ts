// What a message can carry besides its text: images (for vision models) and
// files mentioned with @path, sent as their content.

export const MAX_IMAGES = 4;
// images are scaled down to this many pixels on the long side before sending
const IMAGE_EDGE = 1600;
// below this size (bytes) an image is sent as it is, without decoding it
const SMALL_IMAGE = 1_500_000;
// what one @file adds to a message, at most (characters), and how many files
const FILE_BUDGET = 12000;
const MAX_FILES = 5;

export type ImageAttachment = { name: string; url: string };

const readAsDataUrl = (file: Blob) =>
  new Promise<string>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result));
    r.onerror = () => reject(r.error ?? new Error("read failed"));
    r.readAsDataURL(file);
  });

// imageToDataUrl reads an image file as a data: URL, scaled down (JPEG) when
// it is large; where there is no canvas it is sent as it is.
export async function imageToDataUrl(file: File, edge = IMAGE_EDGE): Promise<string> {
  const original = await readAsDataUrl(file);
  // most screenshots are small enough as they are
  if (file.size < SMALL_IMAGE) return original;
  try {
    const img = await Promise.race([
      new Promise<HTMLImageElement>((resolve, reject) => {
        const el = new Image();
        el.onload = () => resolve(el);
        el.onerror = () => reject(new Error("not an image"));
        el.src = original;
      }),
      new Promise<never>((_, reject) => setTimeout(() => reject(new Error("timeout")), 4000)),
    ]);
    const scale = Math.min(1, edge / Math.max(img.naturalWidth, img.naturalHeight));
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(img.naturalWidth * scale);
    canvas.height = Math.round(img.naturalHeight * scale);
    const g = canvas.getContext("2d");
    if (!g) return original;
    g.drawImage(img, 0, 0, canvas.width, canvas.height);
    return canvas.toDataURL("image/jpeg", 0.86);
  } catch {
    return original;
  }
}

export const isImage = (f: File) => f.type.startsWith("image/");

// ── @file mentions ────────────────────────────────────────────────────────────

// mentionAt finds the @word being typed at the caret: its start and what
// follows the @. "@codebase" is its own feature, not a file.
export function mentionAt(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, caret);
  const m = /(^|\s)@([^\s@]*)$/.exec(before);
  if (!m) return null;
  const query = m[2];
  if ("codebase".startsWith(query) && query.length >= 4) return null;
  return { start: caret - query.length - 1, query };
}

// filterFiles ranks workspace paths for a mention: file-name matches first,
// then path matches.
export function filterFiles(files: string[], query: string, limit = 12): string[] {
  const q = query.toLowerCase();
  if (!q) return files.slice(0, limit);
  const name = (p: string) => p.slice(p.lastIndexOf("/") + 1).toLowerCase();
  const byName = files.filter((p) => name(p).includes(q));
  const byPath = files.filter((p) => !name(p).includes(q) && p.toLowerCase().includes(q));
  return [...byName, ...byPath].slice(0, limit);
}

// mentionedPaths are the @paths in a message that are files of the
// workspace, in order, without repeats.
export function mentionedPaths(text: string, files: Set<string>): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(/(^|\s)@([^\s]+)/g)) {
    const p = m[2].replace(/[.,;:!?)]+$/, "");
    if (files.has(p) && !out.includes(p)) out.push(p);
    if (out.length >= MAX_FILES) break;
  }
  return out;
}

// withFiles puts the mentioned files' content before the message.
export function withFiles(text: string, files: { path: string; content: string }[]): string {
  if (files.length === 0) return text;
  const blocks = files.map((f) => {
    const body = f.content.length > FILE_BUDGET ? `${f.content.slice(0, FILE_BUDGET)}\n…(cut)` : f.content;
    return `<file path="${f.path}">\n${body}\n</file>`;
  });
  return `${blocks.join("\n")}\n\n${text}`;
}

// splitContext takes the attached context back out of a stored message for
// display: the files it carried, whether it had codebase results, and the
// text the user wrote.
export function splitContext(content: string): { text: string; files: string[]; codebase: boolean } {
  const files: string[] = [];
  let text = content.replace(/<file path="([^"]+)">[\s\S]*?<\/file>\s*/g, (_m, p: string) => {
    files.push(p);
    return "";
  });
  let codebase = false;
  text = text.replace(/<codebase-context>[\s\S]*?<\/codebase-context>\s*/g, () => {
    codebase = true;
    return "";
  });
  return { text: text.trim(), files, codebase };
}
