import { formatCountdown } from "../../lib/time";
import styles from "./Countdown.module.css";

/** Props for Countdown. */
export interface CountdownProps {
  iso: string;
  now: Date;
}

/** Relative time to departure: "now", "in 3 min", "in 1 h 5 min". */
export function Countdown({ iso, now }: CountdownProps) {
  const text = formatCountdown(iso, now);
  return <span className={`${styles.countdown} ${text === "now" ? styles.now : ""}`}>{text}</span>;
}
