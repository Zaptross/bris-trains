import type { ReactNode } from "react";
import { Shell } from "./Shell";
import styles from "./SearchTemplate.module.css";

/** Props for SearchTemplate. */
export interface SearchTemplateProps {
  freshness: ReactNode;
  degraded?: boolean;
  search: ReactNode;
  board: ReactNode;
}

/** Departures layout: search above the board on phones, side by side from 900 px. */
export function SearchTemplate({ freshness, degraded, search, board }: SearchTemplateProps) {
  return (
    <Shell freshness={freshness} degraded={degraded}>
      <div className={styles.grid}>
        <section className={styles.search} aria-label="Choose stations">
          {search}
        </section>
        <section className={styles.board} aria-label="Departures">
          {board}
        </section>
      </div>
    </Shell>
  );
}
