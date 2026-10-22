/** Normalises a GTFS route colour ("#D53E45" or "D53E45") to a CSS colour; falls back to the neutral line token. */
export function lineColor(c: string): string {
  const hex = c.trim().replace(/^#/, "");
  return /^([0-9a-f]{3}|[0-9a-f]{6})$/i.test(hex) ? `#${hex.toUpperCase()}` : "var(--sched)";
}
