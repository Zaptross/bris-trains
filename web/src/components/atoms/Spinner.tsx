import styles from "./Spinner.module.css";

/** Props for Spinner. */
export interface SpinnerProps {
  label?: string;
}

/** Loading indicator; static under prefers-reduced-motion. */
export function Spinner({ label = "Loading" }: SpinnerProps) {
  return <span role="status" aria-label={label} className={styles.spinner} />;
}
