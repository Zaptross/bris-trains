import type { Journey, Leg } from "../api/types";

/** Identifies one leg of a journey for the trip page. */
export interface LegRef {
  tripId: string;
  date: string;
  from: string;
  to: string;
}

/** The ref for a leg: its trip, service date and board/alight stations. */
export function legRef(leg: Leg): LegRef {
  return {
    tripId: leg.tripId,
    date: leg.serviceDate,
    from: leg.calls[leg.board]?.stationId ?? "",
    to: leg.calls[leg.alight]?.stationId ?? "",
  };
}

/**
 * URL for leg `index` of a journey: /trip/:tripId?date=&from=&to=, plus for multi-leg journeys
 * `leg=<n>` and one `j=` param per leg so the page can switch between legs.
 */
export function tripHref(legs: readonly LegRef[], index = 0): string {
  const cur = legs[index];
  if (!cur) return "/";
  const p = new URLSearchParams({ date: cur.date, from: cur.from, to: cur.to });
  if (legs.length > 1) {
    p.set("leg", String(index + 1));
    for (const l of legs) p.append("j", [l.tripId, l.date, l.from, l.to].map(encodeURIComponent).join("|"));
  }
  return `/trip/${encodeURIComponent(cur.tripId)}?${p}`;
}

/** Trip page URL for the first leg of a journey. */
export function journeyHref(j: Journey): string {
  return tripHref(j.legs.map(legRef), 0);
}

/** Parsed trip page location. */
export interface TripLocation {
  current: LegRef;
  /** All legs of the journey (length 1 for a direct trip). */
  legs: LegRef[];
  index: number;
}

/** Parses /trip/:tripId search params produced by {@link tripHref}. */
export function parseTripLocation(tripId: string, search: URLSearchParams): TripLocation {
  const current: LegRef = {
    tripId,
    date: search.get("date") ?? "",
    from: search.get("from") ?? "",
    to: search.get("to") ?? "",
  };
  const legs: LegRef[] = [];
  for (const v of search.getAll("j")) {
    const [t, d, f, to] = v.split("|").map((s) => {
      try {
        return decodeURIComponent(s);
      } catch {
        return s;
      }
    });
    if (t) legs.push({ tripId: t, date: d ?? "", from: f ?? "", to: to ?? "" });
  }
  const idx = legs.findIndex((l) => l.tripId === tripId);
  if (legs.length < 2 || idx < 0) return { current, legs: [current], index: 0 };
  return { current, legs, index: idx };
}
