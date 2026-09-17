export function Icon({
  name,
  size = 16,
}: {
  name:
    | "workspace"
    | "chat"
    | "kanban"
    | "terminal"
    | "agents"
    | "skills"
    | "settings"
    | "plus"
    | "search"
    | "pulse"
    | "key"
    | "flag"
    | "box"
    | "cart"
    | "sliders"
    | "palette";
  size?: number;
}) {
  const p = { width: size, height: size, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
  switch (name) {
    case "workspace":
      return <svg {...p}><rect x="4" y="4" width="16" height="16" rx="2.5"/><path d="M4 10h16"/></svg>;
    case "chat":
      return <svg {...p}><path d="M5 6h14a1.5 1.5 0 0 1 1.5 1.5v7A1.5 1.5 0 0 1 19 16H10l-5 3.5V7.5A1.5 1.5 0 0 1 6.5 6Z"/></svg>;
    case "kanban":
      return <svg {...p}><rect x="4" y="5" width="4.5" height="14" rx="1"/><rect x="10" y="5" width="4.5" height="9" rx="1"/><rect x="16" y="5" width="4" height="11" rx="1"/></svg>;
    case "terminal":
      return <svg {...p}><rect x="3.5" y="5" width="17" height="14" rx="2"/><path d="M7 10l3 2-3 2M12 14h5"/></svg>;
    case "agents":
      return <svg {...p}><circle cx="9" cy="8" r="3"/><circle cx="16.5" cy="9.5" r="2.4"/><path d="M4 18c.6-2.6 2.6-4 5-4s4.4 1.4 5 4"/><path d="M14 18c.3-1.6 1.5-2.6 3-2.6 1.4 0 2.5.8 2.8 2.2"/></svg>;
    case "skills":
      return <svg {...p}><path d="M12 3.5l1.7 4.8L18.8 10l-5.1 1.7L12 16.5l-1.7-4.8L5.2 10l5.1-1.7L12 3.5z"/><path d="M18 15.5l.8 2.2 2.2.8-2.2.8L18 21.5l-.8-2.2-2.2-.8 2.2-.8.8-2.2z"/></svg>;
    case "settings":
      return <svg {...p}><circle cx="12" cy="12" r="3"/><path d="M12 3.5v2.2M12 18.3v2.2M4.8 7.2l1.9 1.1M17.3 15.7l1.9 1.1M4.8 16.8l1.9-1.1M17.3 8.3l1.9-1.1"/></svg>;
    case "plus":
      return <svg {...p}><path d="M12 5v14M5 12h14"/></svg>;
    case "search":
      return <svg {...p}><circle cx="11" cy="11" r="6"/><path d="M16 16l4 4"/></svg>;
    case "pulse":
      return <svg {...p}><path d="M3 12h4l2-5 4 10 2-5h6"/></svg>;
    case "key":
      return <svg {...p}><circle cx="8" cy="14" r="3.5"/><path d="M11 12.5l8-8M16.5 4.5l3 3"/></svg>;
    case "flag":
      return <svg {...p}><path d="M6 4v16M6 5h11l-2.2 3.5L17 12H6"/></svg>;
    case "box":
      return <svg {...p}><path d="M4 8l8-4 8 4v9l-8 4-8-4V8z"/><path d="M4 8l8 4 8-4M12 12v9"/></svg>;
    case "cart":
      return <svg {...p}><circle cx="9" cy="20" r="1.4"/><circle cx="17" cy="20" r="1.4"/><path d="M3 4h2l2.2 11.2A1.6 1.6 0 0 0 8.8 16.5h8.7a1.6 1.6 0 0 0 1.55-1.2L21 8H6.2"/></svg>;
    case "sliders":
      return <svg {...p}><path d="M4 8h16M4 16h16"/><circle cx="9" cy="8" r="2.2" fill="currentColor" stroke="none"/><circle cx="15" cy="16" r="2.2" fill="currentColor" stroke="none"/></svg>;
    case "palette":
      return <svg {...p}><path d="M12 3a9 9 0 1 0 0 18h1.6a2.2 2.2 0 0 0 1.7-3.6 2.2 2.2 0 0 1 1.7-3.6H18a3 3 0 0 0 0-6h-.5"/><circle cx="7.5" cy="10" r="1.1" fill="currentColor" stroke="none"/><circle cx="10.5" cy="7.2" r="1.1" fill="currentColor" stroke="none"/><circle cx="14.2" cy="8" r="1.1" fill="currentColor" stroke="none"/><circle cx="8.2" cy="13.5" r="1.1" fill="currentColor" stroke="none"/></svg>;
  }
}
