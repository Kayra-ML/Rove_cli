export type ToastKind = "ok" | "err" | "info";
export type Toast = { id: number; kind: ToastKind; text: string };

type Listener = (list: Toast[]) => void;

let seq = 1;
const items: Toast[] = [];
const listeners = new Set<Listener>();

function emit() {
  const snap = items.slice();
  listeners.forEach((fn) => fn(snap));
}

export function subscribeToasts(fn: Listener): () => void {
  listeners.add(fn);
  fn(items.slice());
  return () => { listeners.delete(fn); };
}

export function toast(text: string, kind: ToastKind = "info") {
  const id = seq++;
  items.push({ id, kind, text });
  if (items.length > 6) items.shift();
  emit();
  window.setTimeout(() => {
    const i = items.findIndex((t) => t.id === id);
    if (i >= 0) {
      items.splice(i, 1);
      emit();
    }
  }, 4200);
}

export function dismissToast(id: number) {
  const i = items.findIndex((t) => t.id === id);
  if (i >= 0) {
    items.splice(i, 1);
    emit();
  }
}
