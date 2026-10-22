import type {
  ErrorResponse,
  HealthResponse,
  JourneysResponse,
  LegResponse,
  ShapeResponse,
  StationsResponse,
} from "./types";

/** A non-2xx API response. `body` is null when the server did not return an ErrorResponse (e.g. a proxy error). */
export class ApiError extends Error {
  readonly status: number;
  readonly body: ErrorResponse | null;

  constructor(status: number, body: ErrorResponse | null) {
    super(body?.message ?? `HTTP ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

function isErrorResponse(v: unknown): v is ErrorResponse {
  if (typeof v !== "object" || v === null) return false;
  const r = v as Record<string, unknown>;
  return typeof r.error === "string" && typeof r.message === "string";
}

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal, headers: { Accept: "application/json" } });
  if (!res.ok) {
    let body: ErrorResponse | null = null;
    try {
      const j: unknown = await res.json();
      if (isErrorResponse(j)) body = j;
    } catch {
      // Non-JSON error body (e.g. dev proxy failure): keep body null.
    }
    throw new ApiError(res.status, body);
  }
  return (await res.json()) as T;
}

/** Options for {@link api.journeys}. */
export interface JourneyQuery {
  origin: string;
  destination: string;
  limit?: number;
  direct?: boolean;
}

/** Options for {@link api.leg}. */
export interface LegQuery {
  tripId: string;
  date: string;
  from: string;
  to: string;
}

/** Typed client for the `trains serve` JSON API (relative paths; Vite proxies /api in dev). */
export const api = {
  stations(signal?: AbortSignal): Promise<StationsResponse> {
    return getJSON("/api/stations", signal);
  },
  journeys(q: JourneyQuery, signal?: AbortSignal): Promise<JourneysResponse> {
    const p = new URLSearchParams({ origin: q.origin, destination: q.destination, limit: String(q.limit ?? 5) });
    if (q.direct) p.set("direct", "1");
    return getJSON(`/api/journeys?${p}`, signal);
  },
  leg(q: LegQuery, signal?: AbortSignal): Promise<LegResponse> {
    const p = new URLSearchParams();
    if (q.date) p.set("date", q.date);
    if (q.from) p.set("from", q.from);
    if (q.to) p.set("to", q.to);
    return getJSON(`/api/legs/${encodeURIComponent(q.tripId)}?${p}`, signal);
  },
  shape(shapeId: string, signal?: AbortSignal): Promise<ShapeResponse> {
    return getJSON(`/api/shapes/${encodeURIComponent(shapeId)}`, signal);
  },
  health(signal?: AbortSignal): Promise<HealthResponse> {
    return getJSON("/api/healthz", signal);
  },
};
