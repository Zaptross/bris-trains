import styles from "./PlatformChip.module.css";

/** Props for PlatformChip. */
export interface PlatformChipProps {
  platform: string | null;
}

/** "P2"; an em dash when the platform is unknown. */
export function PlatformChip({ platform }: PlatformChipProps) {
  if (platform === null || platform === "") {
    return (
      <span className={`${styles.chip} ${styles.none}`} aria-label="platform unknown">
        —
      </span>
    );
  }
  return (
    <span className={styles.chip} aria-label={`platform ${platform}`}>
      P{platform}
    </span>
  );
}
