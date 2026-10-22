import type { Station } from "../api/types";

/** One ranked picker suggestion. `alias` is set when the match came via an alias. */
export interface StationMatch {
  station: Station;
  tier: 1 | 2 | 3 | 4;
  alias?: string;
}

/** Normalises a name or query per SPEC §3.7. */
export function normalise(s: string): string {
  return s
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim()
    .replace(/\s+station$/, "")
    .trim();
}

/** Levenshtein edit distance. */
export function levenshtein(a: string, b: string): number {
  if (a === b) return 0;
  let prev = Array.from({ length: b.length + 1 }, (_, i) => i);
  for (let i = 1; i <= a.length; i++) {
    const cur = [i];
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      cur.push(Math.min((prev[j] ?? 0) + 1, (cur[j - 1] ?? 0) + 1, (prev[j - 1] ?? 0) + cost));
    }
    prev = cur;
  }
  return prev[b.length] ?? 0;
}

const MAX_RESULTS = 8;

function tierFor(q: string, qWords: number, name: string): 1 | 2 | 3 | 4 | 0 {
  if (name === q) return 1;
  if (name.startsWith(q)) return 2;
  if (` ${name}`.includes(` ${q}`)) return 3;
  const head = name.split(" ").slice(0, qWords).join(" ");
  if (levenshtein(q, name) <= 2 || levenshtein(q, head) <= 2) return 4;
  return 0;
}

/**
 * Ranks stations for a picker query using the SPEC §3.7 tiers (exact incl. aliases → prefix →
 * word-boundary substring → Levenshtein ≤ 2). Tiers 1–3 come first in tier order then alphabetical,
 * then tier 4; capped at 8. Empty query returns [].
 */
export function matchStations(query: string, stations: readonly Station[]): StationMatch[] {
  const q = normalise(query);
  if (q === "") return [];
  const qWords = q.split(" ").length;
  const best = new Map<string, StationMatch>();
  for (const station of stations) {
    const candidates: { text: string; alias?: string }[] = [{ text: normalise(station.name) }];
    for (const a of station.aliases) candidates.push({ text: normalise(a), alias: a });
    for (const c of candidates) {
      const t = tierFor(q, qWords, c.text);
      if (t === 0) continue;
      const prev = best.get(station.id);
      if (!prev || t < prev.tier) {
        best.set(station.id, c.alias === undefined ? { station, tier: t } : { station, tier: t, alias: c.alias });
      }
    }
  }
  return [...best.values()]
    .sort((a, b) => a.tier - b.tier || a.station.name.localeCompare(b.station.name))
    .slice(0, MAX_RESULTS);
}
