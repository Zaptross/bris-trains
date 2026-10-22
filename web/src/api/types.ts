// Mirror of gtfsrail/wire (Go). Field names and nullability must match exactly.
// Timestamps are RFC3339 in Australia/Brisbane; durations are integer seconds.

export type Punctuality =
  | "early"
  | "onTime"
  | "late"
  | "veryLate"
  | "skipping"
  | "cancelled"
  | "scheduled";

export type Position = "passed" | "current" | "upcoming";

export type TimeSource = "observed" | "predicted" | "projected" | "scheduled";

export interface Station {
  id: string;
  name: string;
  lat: number;
  lon: number;
  aliases: string[];
}

export interface Call {
  stationId: string;
  name: string;
  platform: string | null;
  scheduled: string;
  expected: string;
  actual: string | null;
  position: Position;
  source: TimeSource;
  skipped: boolean;
}

export interface VehicleFix {
  lat: number;
  lon: number;
  atSequence: number;
  /** Index into Leg.calls of the call at atSequence, or null if not in the leg. */
  atIndex: number | null;
  status: "STOPPED_AT" | "IN_TRANSIT_TO" | "INCOMING_AT";
  observedAt: string;
  stale: boolean;
}

export interface Alert {
  id: string;
  header: string;
  description: string;
  effect: string;
  cause: string;
  activeUntil: string | null;
}

export interface Leg {
  tripId: string;
  serviceDate: string; // YYYY-MM-DD
  runNumber: string | null;
  routeName: string;
  routeColor: string;
  headsign: string;
  calls: Call[];
  board: number;
  alight: number;
  punctuality: Punctuality;
  delaySeconds: number;
  vehicle: VehicleFix | null;
  alerts: Alert[];
  shapeId: string | null;
}

export interface Change {
  stationId: string;
  name: string;
  bufferSeconds: number;
  tight: boolean;
}

export interface Journey {
  legs: Leg[];
  change: Change | null;
  depart: string;
  arrive: string;
  punctuality: Punctuality;
}

export interface JourneysResponse {
  origin: Station;
  destination: Station;
  journeys: Journey[];
  generatedAt: string;
  dataAgeSeconds: number;
  degraded: boolean;
}

export interface LegResponse {
  leg: Leg;
  generatedAt: string;
  dataAgeSeconds: number;
  degraded: boolean;
}

export interface ShapeResponse {
  shapeId: string;
  points: [number, number][]; // [lat, lon]
}

export interface StationsResponse {
  stations: Station[];
}

export type ErrorCode =
  | "ambiguous"
  | "unknown_station"
  | "not_found"
  | "bad_request"
  | "method_not_allowed"
  | "internal";

export interface ErrorResponse {
  error: ErrorCode;
  message: string;
  candidates?: Station[];
}

export interface FeedHealth {
  lastSuccess: string | null;
  ageSeconds: number | null;
  lastError: string | null;
  fetches: number;
  /** Conditional GETs answered 304 (data unchanged upstream). */
  notModified: number;
}

export interface HealthResponse {
  tripUpdates: FeedHealth;
  vehiclePositions: FeedHealth;
  alerts: FeedHealth;
  degraded: boolean;
}
