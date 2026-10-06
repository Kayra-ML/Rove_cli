// Node 25+ ships its own global localStorage, which is undefined unless node
// runs with --localstorage-file, and it shadows jsdom's. Put jsdom's back so
// app code that uses the bare global works under test.
const dom = (globalThis as { jsdom?: { window: Window } }).jsdom;
if (dom && typeof globalThis.localStorage === "undefined") {
  Object.defineProperty(globalThis, "localStorage", { value: dom.window.localStorage, configurable: true });
  Object.defineProperty(globalThis, "sessionStorage", { value: dom.window.sessionStorage, configurable: true });
}
