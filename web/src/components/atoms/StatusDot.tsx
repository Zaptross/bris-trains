import type { Punctuality } from "../../api/types";
import { punctualityInfo } from "../../lib/punctuality";
import styles from "./StatusDot.module.css";

/** Props for StatusDot. */
export interface StatusDotProps {
  punctuality: Punctuality;
  delaySeconds?: number;
  /** Render the text label beside the glyph. */
  showLabel?: boolean;
}

/** Punctuality glyph (● ▲ ▼ ○ ✕) in its tone; label always available to assistive tech. */
export function StatusDot({ punctuality, delaySeconds = 0, showLabel }: StatusDotProps) {
  const info = punctualityInfo(punctuality, delaySeconds);
  return (
    <span className={`${styles.dot} ${styles[info.tone]}`} title={info.label}>
      <span aria-hidden="true" className={styles.glyph}>{info.glyph}</span>
      {showLabel ? <span>{info.label}</span> : <span className={styles.sr}>{info.label}</span>}
    </span>
  );
}
