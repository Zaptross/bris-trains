import styles from "./StopsRollup.module.css";

/** Props for StopsRollup. */
export interface StopsRollupProps {
  /** Number of stops folded away. */
  count: number;
  expanded: boolean;
  onToggle(): void;
  /** Which end of the trip the stops are at; draws the rail toward the visible stops. */
  side: "before" | "after";
  /** Where the folded stops sit relative to the train, for the rail style. */
  passed: boolean;
  /** The train is currently within the folded stops. */
  trainHere?: boolean;
  /** Station the folded stops lead to or from, for the accessible label. */
  anchor: string;
}

/** Timeline row that folds the stops before boarding or after alighting into "12 stops…". */
export function StopsRollup({ count, expanded, onToggle, side, passed, trainHere, anchor }: StopsRollupProps) {
  const noun = `${count} stop${count === 1 ? "" : "s"}`;
  const where = side === "before" ? `before ${anchor}` : `after ${anchor}`;
  return (
    <li className={`${styles.row} ${styles[side]} ${passed ? styles.passed : ""}`}>
      <span className={styles.rail} aria-hidden="true" />
      <button type="button" className={styles.toggle} aria-expanded={expanded} onClick={onToggle} aria-label={`${expanded ? "Hide" : "Show"} ${noun} ${where}`}>
        <span className={styles.label}>{expanded ? `Hide ${noun}` : `${noun}…`}</span>
        {trainHere && !expanded && (
          <span className={styles.train}>
            <span className={styles.pip} aria-hidden="true" /> train here
          </span>
        )}
      </button>
    </li>
  );
}
