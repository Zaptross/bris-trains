import type { CSSProperties } from "react";
import type { Leg } from "../../api/types";
import { lineColor } from "../../lib/color";
import type { LegProgress } from "../../lib/progress";
import { TimelineNode } from "../molecules/TimelineNode";
import styles from "./TripTimeline.module.css";

/** Props for TripTimeline. */
export interface TripTimelineProps {
  leg: Leg;
  /** From legProgress(); null when no position is known. */
  progress: LegProgress | null;
}

/** Vertical stop-by-stop timeline with the train marker on the current call. */
export function TripTimeline({ leg, progress }: TripTimelineProps) {
  return (
    <ol className={styles.timeline} style={{ "--line": lineColor(leg.routeColor) } as CSSProperties} aria-label="Stops">
      {leg.calls.map((c, i) => (
        <TimelineNode
          key={`${i}-${c.stationId}`}
          call={c}
          isFirst={i === 0}
          isLast={i === leg.calls.length - 1}
          isBoard={i === leg.board}
          isAlight={i === leg.alight}
          vehicle={
            progress && progress.index === i
              ? { status: progress.status, fraction: progress.fraction, stale: progress.stale, estimated: progress.estimated }
              : undefined
          }
        />
      ))}
    </ol>
  );
}
