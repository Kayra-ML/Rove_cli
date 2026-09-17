import { useEffect, useRef } from "react";
import type { TerminalSession } from "~/lib/types";
import { rpc, subscribeEvents } from "~/lib/rpc";
import "@xterm/xterm/css/xterm.css";

interface Props {
  sessions: TerminalSession[];
  activeId: string | null;
  onSelect: (id: string) => void;
  onSpawn: () => void;
  onRestart?: (id: string) => void;
  onDetach?: (id: string) => void;
  hideTabs?: boolean;
  themeKey?: string;
}

export function Terminal({ sessions, activeId, onSelect, onSpawn, onRestart, onDetach, hideTabs, themeKey }: Props) {
  const termRef = useRef<HTMLDivElement>(null);
  const disposeRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!termRef.current || !activeId) return;
    let disposed = false;
    let unsub: (() => void) | null = null;

    void (async () => {
      type TermInst = {
        open(el: HTMLElement): void;
        write(data: string): void;
        reset(): void;
        dispose(): void;
        loadAddon(a: unknown): void;
        onData(h: (d: string) => void): { dispose(): void };
        onResize(h: (s: { cols: number; rows: number }) => void): { dispose(): void };
        cols: number;
        rows: number;
      };
      let xterm: { Terminal: new (o: object) => TermInst } | null = null;
      try {
        xterm = await import("@xterm/xterm");
      } catch {
        return;
      }
      if (!xterm || disposed || !termRef.current) return;

      const cs = getComputedStyle(document.documentElement);
      const term = new xterm.Terminal({
        theme: {
          background: cs.getPropertyValue("--bg-sunken").trim() || "#0c0d10",
          foreground: cs.getPropertyValue("--text").trim() || "#eceef2",
          cursor: cs.getPropertyValue("--accent").trim() || "#7aa2ff",
          selectionBackground: cs.getPropertyValue("--bg-hover").trim() || "#262b36",
        },
        fontFamily: "ui-monospace, 'IBM Plex Mono', Menlo, monospace",
        fontSize: 13,
        cursorBlink: true,
      });

      let fitFn: (() => void) | null = null;
      try {
        const FitAddon = await import("@xterm/addon-fit");
        const fit = new FitAddon.FitAddon();
        term.loadAddon(fit);
        term.open(termRef.current);
        fit.fit();
        fitFn = () => {
          try {
            fit.fit();
          } catch {
            /* ignore */
          }
        };
      } catch {
        term.open(termRef.current);
      }

      const dataDisp = term.onData((data) => {
        void rpc("terminal.write", { id: activeId, data });
      });
      const resizeDisp = term.onResize(({ cols, rows }) => {
        void rpc("terminal.resize", { id: activeId, cols, rows });
      });

      try {
        const attached = await rpc<{ replay?: string }>("terminal.attach", { id: activeId });
        if (attached?.replay && !disposed) term.write(attached.replay);
      } catch {
        /* attach may fail if PTY already gone */
      }

      unsub = subscribeEvents(`terminal.${activeId}`, (ev) => {
        if (ev.type === "terminal.data") {
          const p = ev.payload as { id?: string; data?: string };
          if (p?.data) term.write(p.data);
        }
      });

      if (fitFn && termRef.current) {
        const ro = new ResizeObserver(fitFn);
        ro.observe(termRef.current);
        disposeRef.current = () => {
          ro.disconnect();
          dataDisp.dispose();
          resizeDisp.dispose();
          term.dispose();
        };
      } else {
        disposeRef.current = () => {
          dataDisp.dispose();
          resizeDisp.dispose();
          term.dispose();
        };
      }

      if (fitFn) {
        void rpc("terminal.resize", { id: activeId, cols: term.cols, rows: term.rows });
      }
    })();

    return () => {
      disposed = true;
      unsub?.();
      disposeRef.current?.();
      disposeRef.current = null;
    };
  }, [activeId, themeKey]);

  return (
    <div
      className="term-wrap"
      onContextMenu={(e) => {
        e.preventDefault();
        onSpawn();
      }}
    >
      {!hideTabs && (
        <div className="term-tabs">
          <button
            className="icon-btn"
            title="+"
            onClick={onSpawn}
            onContextMenu={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onSpawn();
            }}
          >
            +
          </button>
          {sessions.map((s) => (
            <button
              key={s.id}
              className={activeId === s.id ? "term-tab active" : "term-tab"}
              onClick={() => onSelect(s.id)}
            >
              {s.title || "shell"}
              <span className={s.status === "running" ? "pip on" : "pip"} style={{ marginLeft: 8 }} />
            </button>
          ))}
        </div>
      )}
      {activeId && (onRestart || onDetach) && (
        <div className="term-toolbar">
          {onRestart && (
            <button type="button" className="ghost" style={{ fontSize: 11 }} onClick={() => onRestart(activeId)}>restart</button>
          )}
          {onDetach && (
            <button type="button" className="ghost" style={{ fontSize: 11 }} onClick={() => onDetach(activeId)}>detach</button>
          )}
        </div>
      )}
      <div
        ref={termRef}
        className="term-host"
        style={{ height: "100%", background: "var(--bg-sunken)" }}
      />
    </div>
  );
}
