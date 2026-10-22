import type { ReactNode } from "react";
import { Shell } from "./Shell";
import styles from "./TripTemplate.module.css";

/** Props for TripTemplate. */
export interface TripTemplateProps {
  freshness: ReactNode;
  degraded?: boolean;
  header: ReactNode;
  timeline: ReactNode;
  /** Line diagram; omitted when the trip has no shape. */
  diagram?: ReactNode;
}

/** Trip layout: header, then timeline then diagram on phones, side by side from 900 px. */
export function TripTemplate({ freshness, degraded, header, timeline, diagram }: TripTemplateProps) {
  return (
    <Shell freshness={freshness} degraded={degraded}>
      <div className={styles.page}>
        {header}
        <div className={`${styles.grid} ${diagram ? styles.two : ""}`}>
          <section className={styles.timeline} aria-label="Timeline">
            {timeline}
          </section>
          {diagram && <section className={styles.diagram} aria-label="Line diagram">{diagram}</section>}
        </div>
      </div>
    </Shell>
  );
}
