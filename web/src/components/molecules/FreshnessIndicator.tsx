import { formatDuration } from "../../lib/time";
import styles from "./FreshnessIndicator.module.css";

/** Props for FreshnessIndicator. */
export interface FreshnessIndicatorProps {
  generatedAt: string;
  dataAgeSeconds: number;
  degraded: boolean;
  now: Date;
}

const STALE_AFTER_S = 90;

/** "live · 7s old", or "schedule only" when realtime is unavailable. Age keeps counting between polls. */
export function FreshnessIndicator({ generatedAt, dataAgeSeconds, degraded, now }: FreshnessIndicatorProps) {
  if (degraded) {
    return (
      <span className={`${styles.fresh} ${styles.degraded}`} role="status">
        <span aria-hidden="true">○</span> schedule only — realtime unavailable
      </span>
    );
  }
  const gen = new Date(generatedAt).getTime();
  const elapsed = Number.isNaN(gen) ? 0 : Math.max(0, (now.getTime() - gen) / 1000);
  const age = Math.round(dataAgeSeconds + elapsed);
  const stale = age > STALE_AFTER_S;
  return (
    <span className={`${styles.fresh} ${stale ? styles.stale : styles.live}`} role="status">
      <span aria-hidden="true" className={styles.pip} />
      live · <span className={styles.age}>{formatDuration(age)}</span> old
    </span>
  );
}
