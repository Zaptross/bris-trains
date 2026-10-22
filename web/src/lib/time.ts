const hm = new Intl.DateTimeFormat("en-AU", {
  timeZone: "Australia/Brisbane",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

const MINUS = "\u2212";

/** Formats an ISO timestamp as HH:MM in Australia/Brisbane. Returns "--:--" for unparseable input. */
export function formatHM(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "--:--";
  // Some engines render midnight as "24"; normalise.
  return hm.format(d).replace(/^24/, "00");
}

/** Seconds from a to b (b − a), rounded. NaN-safe: returns 0 on bad input. */
export function secondsBetween(a: string, b: string): number {
  const d = (new Date(b).getTime() - new Date(a).getTime()) / 1000;
  return Number.isFinite(d) ? Math.round(d) : 0;
}

/** Signed offset: "+34s", "−34s" under a minute; "+2 min", "−1 min" otherwise; "0s" for zero. */
export function formatOffset(seconds: number): string {
  if (seconds === 0) return "0s";
  const sign = seconds > 0 ? "+" : MINUS;
  const abs = Math.abs(seconds);
  if (abs < 60) return `${sign}${abs}s`;
  return `${sign}${Math.round(abs / 60)} min`;
}

/** Human duration: "45s", "7 min", "1 h 5 min". */
export function formatDuration(seconds: number): string {
  const abs = Math.abs(Math.round(seconds));
  if (abs < 60) return `${abs}s`;
  const mins = Math.round(abs / 60);
  if (mins < 60) return `${mins} min`;
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

/** Countdown from now to iso: "now" within 30s either side, "in 3 min", "in 1 h 5 min", or "3 min ago". */
export function formatCountdown(iso: string, now: Date): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const s = Math.round((t - now.getTime()) / 1000);
  if (Math.abs(s) <= 30) return "now";
  if (s < 0) return `${formatDuration(Math.max(60, -s))} ago`;
  return `in ${formatDuration(Math.max(60, s))}`;
}
