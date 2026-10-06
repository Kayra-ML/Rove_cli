import type React from "react";
import cloud from "~/assets/appicon-256.png";
import { t, type Lang } from "~/lib/i18n";

// Thinking is what the chat shows between your message and the first word of
// the answer: the cloud from the app's own mark, with its terminal cursor
// blinking and a sweep running along its lower edge, and a quiet word for
// what the agent is doing. Before this the only sign of life was a line in
// the message box, far from where you were looking.
//
// The cloud is the icon with everything but the cloud clipped away, so the
// dark tile behind it does not show as a square on the page.
export function Thinking({ label, lang }: { label?: string; lang: Lang }) {
  return (
    <div className="thinking" role="status" aria-live="polite">
      <ThinkCloud />
      <span className="think-word">{label || t("thinking", lang)}</span>
    </div>
  );
}

// ThinkCloud is the mark alone, thinking: the chat shows it beside a word,
// the terminal at the head of its working line. width is the cloud's in px.
export function ThinkCloud({ width }: { width?: number }) {
  return (
    <span className="think-cloud" aria-hidden style={width ? ({ "--cw": `${width}px` } as React.CSSProperties) : undefined}>
      <img src={cloud} alt="" />
      <span className="think-cursor" />
      <span className="think-wave" />
    </span>
  );
}
