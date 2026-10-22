import { formatHM } from "../../lib/time";
import styles from "./TimeLabel.module.css";

/** Props for TimeLabel. */
export interface TimeLabelProps {
  iso: string;
  size?: "sm" | "md" | "lg";
  muted?: boolean;
  struck?: boolean;
}

/** HH:MM in Australia/Brisbane, monospace tabular numerals. */
export function TimeLabel({ iso, size = "md", muted, struck }: TimeLabelProps) {
  const cls = [styles.time, styles[size], muted && styles.muted, struck && styles.struck].filter(Boolean).join(" ");
  return (
    <time dateTime={iso} className={cls}>
      {formatHM(iso)}
    </time>
  );
}
