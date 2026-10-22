import type { Station } from "../api/types";

const KEY = "trains:last-pair";

/** The last origin/destination pair, resolved against the current station list. */
export interface StationPair {
  origin: Station | null;
  destination: Station | null;
}

/** Restores the last pair from localStorage; unknown IDs resolve to null. */
export function loadLastPair(stations: readonly Station[]): StationPair {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return { origin: null, destination: null };
    const v: unknown = JSON.parse(raw);
    const r = typeof v === "object" && v !== null ? (v as Record<string, unknown>) : {};
    const find = (id: unknown) => (typeof id === "string" ? (stations.find((s) => s.id === id) ?? null) : null);
    return { origin: find(r.origin), destination: find(r.destination) };
  } catch {
    return { origin: null, destination: null };
  }
}

/** Persists the pair's station IDs. Storage failures (private mode, quota) are ignored. */
export function saveLastPair(pair: StationPair): void {
  try {
    localStorage.setItem(KEY, JSON.stringify({ origin: pair.origin?.id ?? null, destination: pair.destination?.id ?? null }));
  } catch {
    // Persistence is best-effort.
  }
}
