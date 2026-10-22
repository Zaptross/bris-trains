import type { Change } from "../../api/types";
import { formatDuration } from "../../lib/time";
import styles from "./ChangeSummary.module.css";

/** Props for ChangeSummary. */
export interface ChangeSummaryProps {
  change: Change;
}

/** "via Bowen Hills · 7 min change", flagged when the connection is tight. */
export function ChangeSummary({ change }: ChangeSummaryProps) {
  const name = change.name.replace(/ station$/i, "");
  return (
    <span className={`${styles.change} ${change.tight ? styles.tight : ""}`}>
      {change.tight && <span aria-hidden="true">⚠ </span>}
      via {name} · <span className={styles.buffer}>{formatDuration(change.bufferSeconds)}</span> change
      {change.tight && <span className={styles.tag}> tight</span>}
    </span>
  );
}
