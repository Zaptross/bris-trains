import type { ReactNode } from "react";
import type { Tone } from "../../lib/punctuality";
import styles from "./Badge.module.css";

/** Props for Badge. */
export interface BadgeProps {
  tone?: Tone;
  children: ReactNode;
}

/** Small outlined label. */
export function Badge({ tone = "default", children }: BadgeProps) {
  return <span className={`${styles.badge} ${styles[tone]}`}>{children}</span>;
}
