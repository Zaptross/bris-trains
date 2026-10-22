import type { Station } from "../../api/types";
import { saveLastPair } from "../../lib/lastPair";
import { Button } from "../atoms/Button";
import { StationPicker } from "../molecules/StationPicker";
import styles from "./JourneySearchForm.module.css";

/** Props for JourneySearchForm. */
export interface JourneySearchFormProps {
  stations: Station[];
  origin: Station | null;
  destination: Station | null;
  onChange(origin: Station | null, destination: Station | null): void;
}

/** Origin/destination pickers with a swap control; persists the pair to localStorage (`trains:last-pair`). */
export function JourneySearchForm({ stations, origin, destination, onChange }: JourneySearchFormProps) {
  function update(o: Station | null, d: Station | null) {
    saveLastPair({ origin: o, destination: d });
    onChange(o, d);
  }
  return (
    <form className={styles.form} role="search" onSubmit={(e) => e.preventDefault()}>
      <StationPicker label="From" stations={stations} value={origin} onChange={(s) => update(s, destination)} placeholder="e.g. central" />
      <div className={styles.swapRow}>
        <Button
          variant="ghost"
          aria-label="Swap origin and destination"
          title="Swap"
          onClick={() => update(destination, origin)}
          disabled={!origin && !destination}
        >
          ⇅ Swap
        </Button>
      </div>
      <StationPicker label="To" stations={stations} value={destination} onChange={(s) => update(origin, s)} placeholder="e.g. roma street" />
      {origin && destination && origin.id === destination.id && (
        <p className={styles.warn} role="alert">
          Pick two different stations.
        </p>
      )}
    </form>
  );
}
