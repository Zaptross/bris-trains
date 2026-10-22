import type { Call, Leg, VehicleFix } from "../api/types";

/** Where the train is relative to the leg's calls. */
export interface LegProgress {
  /** Index into Leg.calls of the current call (the stop it is at, or heading to). */
  index: number;
  /** Index of the previous stopping call, or null when the current call is the first. */
  prevIndex: number | null;
  status: VehicleFix["status"];
  /** 0..1 from the previous stopping call to the current call. */
  fraction: number;
  /** True when no fresh vehicle fix exists and the position is interpolated on time. */
  estimated: boolean;
  /** True when a vehicle fix exists but is stale. */
  stale: boolean;
}

/** Fraction of the way from `fromIso` to `toIso` at `now`, clamped to 0..1. */
export function timeFraction(fromIso: string, toIso: string, now: Date): number {
  const a = new Date(fromIso).getTime();
  const b = new Date(toIso).getTime();
  if (!Number.isFinite(a) || !Number.isFinite(b) || b <= a) return 1;
  return Math.min(1, Math.max(0, (now.getTime() - a) / (b - a)));
}

function departure(c: Call): string {
  return c.actual ?? c.expected;
}

/** The fresh (non-stale) vehicle fix, or null. */
export function freshFix(leg: Leg): VehicleFix | null {
  return leg.vehicle && !leg.vehicle.stale ? leg.vehicle : null;
}

/**
 * Locates the train on a leg. With a fresh fix, STOPPED_AT snaps to the current call and
 * IN_TRANSIT_TO / INCOMING_AT is placed by time between the previous stopping call and the current one.
 * Without a fresh fix the same time interpolation applies and the result is marked estimated.
 * Returns null when no position is known: no call is current (the trip has finished), or there is
 * no fresh fix and the first call's expected time is still in the future.
 */
export function legProgress(leg: Leg, now: Date): LegProgress | null {
  const index = leg.calls.findIndex((c) => c.position === "current");
  const cur = leg.calls[index];
  if (index < 0 || !cur) return null;
  let prevIndex: number | null = null;
  for (let j = index - 1; j >= 0; j--) {
    if (!leg.calls[j]?.skipped) {
      prevIndex = j;
      break;
    }
  }
  const prev = prevIndex === null ? undefined : leg.calls[prevIndex];
  const fix = freshFix(leg);
  const stale = leg.vehicle?.stale ?? false;
  if (fix && fix.status === "STOPPED_AT") {
    return { index, prevIndex, status: "STOPPED_AT", fraction: 1, estimated: false, stale };
  }
  // No fix and the train hasn't reached its first stop yet: there is nothing to estimate.
  if (!fix && !prev && now.getTime() < new Date(cur.expected).getTime()) return null;
  const fraction = prev ? timeFraction(departure(prev), cur.expected, now) : 1;
  if (fix) return { index, prevIndex, status: fix.status, fraction, estimated: false, stale };
  return {
    index,
    prevIndex,
    status: prev && fraction < 1 ? "IN_TRANSIT_TO" : "STOPPED_AT",
    fraction,
    estimated: true,
    stale,
  };
}
