import type { Punctuality } from "../../api/types";
import { punctualityInfo } from "../../lib/punctuality";
import { formatOffset } from "../../lib/time";
import styles from "./DelayChip.module.css";

/** Props for DelayChip. */
export interface DelayChipProps {
  punctuality: Punctuality;
  delaySeconds: number;
}

/** Signed delay ("+34s", "+2 min", "−1 min", "on time"). Renders nothing without live timing. */
export function DelayChip({ punctuality, delaySeconds }: DelayChipProps) {
  if (punctuality === "scheduled" || punctuality === "cancelled" || punctuality === "skipping") return null;
  const { tone } = punctualityInfo(punctuality, delaySeconds);
  const text = punctuality === "onTime" && Math.abs(delaySeconds) < 30 ? "on time" : formatOffset(delaySeconds);
  return <span className={`${styles.chip} ${styles[tone]}`}>{text}</span>;
}
