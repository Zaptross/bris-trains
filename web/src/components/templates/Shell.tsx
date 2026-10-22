import type { ReactNode } from "react";
import { Link } from "react-router";
import styles from "./Shell.module.css";

/** Props for Shell. */
export interface ShellProps {
  freshness: ReactNode;
  degraded?: boolean;
  children: ReactNode;
}

/** Page frame: top bar with brand and always-visible freshness, content, attribution footer. */
export function Shell({ freshness, degraded, children }: ShellProps) {
  return (
    <div className={`${styles.shell} ${degraded ? styles.degraded : ""}`}>
      <header className={styles.bar}>
        <Link to="/" className={styles.brand}>
          Trains <span className={styles.city}>Brisbane</span>
        </Link>
        <span className={styles.fresh}>{freshness}</span>
      </header>
      <main className={styles.main}>{children}</main>
      <footer className={styles.footer}>
        Data © <a href="https://www.translink.com.au/about-translink/open-data">Translink</a>, licensed{" "}
        <a href="https://creativecommons.org/licenses/by/4.0/">CC BY 4.0</a>.
      </footer>
    </div>
  );
}
