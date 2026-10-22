import { QueryClient, keepPreviousData, useQuery } from "@tanstack/react-query";
import { ApiError, api } from "./client";
import type { LegQuery } from "./client";

/** Realtime poll interval for journeys, legs and health. */
export const POLL_MS = 15_000;

/** App-wide query client: client errors (4xx) are not retried. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
        refetchOnWindowFocus: true,
      },
    },
  });
}

/** All rail stations; changes at most daily. */
export function useStations() {
  return useQuery({
    queryKey: ["stations"],
    queryFn: ({ signal }) => api.stations(signal),
    staleTime: 60 * 60 * 1000,
  });
}

/** Next 5 journeys between two station IDs, polled every 15 s without flicker. */
export function useJourneys(origin: string | null, destination: string | null) {
  return useQuery({
    queryKey: ["journeys", origin, destination],
    queryFn: ({ signal }) => {
      if (!origin || !destination) throw new Error("journeys query needs both stations");
      return api.journeys({ origin, destination, limit: 5 }, signal);
    },
    enabled: Boolean(origin && destination && origin !== destination),
    refetchInterval: POLL_MS,
    placeholderData: keepPreviousData,
  });
}

/** One leg with stop-by-stop detail, polled every 15 s. */
export function useLeg(q: LegQuery) {
  return useQuery({
    queryKey: ["leg", q.tripId, q.date, q.from, q.to],
    queryFn: ({ signal }) => api.leg(q, signal),
    enabled: q.tripId !== "",
    refetchInterval: POLL_MS,
    placeholderData: keepPreviousData,
  });
}

/** A trip shape; immutable per ID. */
export function useShape(shapeId: string | null) {
  return useQuery({
    queryKey: ["shape", shapeId],
    queryFn: ({ signal }) => {
      if (!shapeId) throw new Error("shape query needs an ID");
      return api.shape(shapeId, signal);
    },
    enabled: shapeId !== null,
    staleTime: Infinity,
    gcTime: Infinity,
  });
}

/** Feed health, used for freshness before any journey/leg data exists. */
export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: ({ signal }) => api.health(signal),
    refetchInterval: POLL_MS,
  });
}
