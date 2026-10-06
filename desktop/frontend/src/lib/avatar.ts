// Monochrome initials for character/profile badges — no emoji. Colorful
// pictograms render as OS emoji art and clash with the app's flat icon
// theme; two letters in a plain badge read as a proper product identity
// mark instead.

export function initialsOf(name: string): string {
  const words = name
    .trim()
    .split(/\s+/)
    .filter((w) => /\p{L}/u.test(w));
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}
