import type { Journey } from "../../api/types";
import { ApiError } from "../../api/client";
import { JourneyRow } from "../molecules/JourneyRow";
import styles from "./DepartureBoard.module.css";

/** Props for DepartureBoard. */
export interface DepartureBoardProps {
  /** Journeys for the current pair; undefined while the first response is pending. */
  journeys: Journey[] | undefined;
  loading: boolean;
  error: unknown;
  degraded: boolean;
  now: Date;
  onSelect(j: Journey): void;
  /** Rows shown as loading skeletons. */
  rows?: number;
}

function ErrorState({ error }: { error: unknown }) {
  const body = error instanceof ApiError ? error.body : null;
  const names = (body?.candidates ?? []).map((s) => s.name);
  let title = "Couldn’t load departures";
  if (body?.error === "ambiguous") title = "That station name is ambiguous";
  else if (body?.error === "unknown_station") title = "Unknown station";
  return (
    <div className={styles.state} role="alert">
      <p className={styles.title}>{title}</p>
      <p className={styles.detail}>{error instanceof Error ? error.message : String(error)}</p>
      {names.length > 0 && (
        <p className={styles.detail}>
          {body?.error === "ambiguous" ? "Matches: " : "Did you mean: "}
          {names.join(", ")}
        </p>
      )}
    </div>
  );
}

/** Departure board: up to 5 JourneyRows with skeleton, empty, error and degraded states. */
export function DepartureBoard({ journeys, loading, error, degraded, now, onSelect, rows = 5 }: DepartureBoardProps) {
  if (error && !journeys) return <ErrorState error={error} />;
  if (!journeys) {
    if (!loading) return null;
    return (
      <ol className={styles.list} aria-busy="true" aria-label="Loading departures">
        {Array.from({ length: rows }, (_, i) => (
          <li key={i} className={styles.skeleton} />
        ))}
      </ol>
    );
  }
  if (journeys.length === 0) {
    return (
      <div className={styles.state}>
        <p className={styles.title}>No trains in the next 24 hours</p>
        <p className={styles.detail}>There is no direct or one-change service between these stations in that window.</p>
      </div>
    );
  }
  return (
    <ol className={styles.list} aria-label="Departures">
      {journeys.map((j) => (
        <li key={j.legs.map((l) => `${l.tripId}@${l.serviceDate}`).join("+")}>
          <JourneyRow journey={j} now={now} degraded={degraded} onSelect={() => onSelect(j)} />
        </li>
      ))}
    </ol>
  );
}
