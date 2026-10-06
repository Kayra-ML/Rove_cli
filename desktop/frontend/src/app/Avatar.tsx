import { Icon } from "./Icons";

interface Props {
  // The character's or profile's name. Omit for "no character" — renders a
  // plain agent glyph instead of initials.
  name?: string;
  size?: "xs" | "sm" | "md";
  // Tints the badge with the accent color, for the one badge showing the
  // session's current identity (chat bar, terminal mode line).
  accent?: boolean;
}

const ICON_PX: Record<NonNullable<Props["size"]>, number> = { xs: 11, sm: 13, md: 18 };

// Avatar draws no picture for a named character or profile: a tile of
// initials beside the name it repeats read as a stock "profile picture",
// and every place that shows one shows the name next to it. Only "no
// character" keeps a plain agent glyph, where there is no name to show.
export function Avatar({ name, size = "sm", accent }: Props) {
  if (name) return null;
  return (
    <span className={`avatar avatar-${size} avatar-icon${accent ? " avatar-accent" : ""}`}>
      <Icon name="agents" size={ICON_PX[size]} />
    </span>
  );
}
