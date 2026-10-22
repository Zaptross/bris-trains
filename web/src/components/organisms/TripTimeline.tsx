import { useState, type CSSProperties } from "react";
import type { Leg } from "../../api/types";
import { lineColor } from "../../lib/color";
import type { LegProgress } from "../../lib/progress";
import { StopsRollup } from "../molecules/StopsRollup";
import { TimelineNode } from "../molecules/TimelineNode";
import styles from "./TripTimeline.module.css";

/** Props for TripTimeline. */
export interface TripTimelineProps {
  leg: Leg;
  /** From legProgress(); null when no position is known. */
  progress: LegProgress | null;
}

/**
 * Vertical stop-by-stop timeline with the train marker on the current call.
 * Stops before boarding and after alighting are folded into "12 stops…"
 * rows until expanded.
 */
export function TripTimeline({ leg, progress }: TripTimelineProps) {
  const { calls, board, alight } = leg;
  // Folding is per trip: switching legs starts collapsed again.
  const [open, setOpen] = useState({ trip: leg.tripId, before: false, after: false });
  const state = open.trip === leg.tripId ? open : { trip: leg.tripId, before: false, after: false };
  const toggle = (side: "before" | "after") => setOpen({ ...state, [side]: !state[side] });

  const at = progress?.index ?? -1;
  const node = (i: number) => {
    const c = calls[i];
    if (!c) return null;
    return (
      <TimelineNode
        key={`${i}-${c.stationId}`}
        call={c}
        isFirst={i === 0}
        isLast={i === calls.length - 1}
        isBoard={i === board}
        isAlight={i === alight}
        vehicle={
          progress && at === i
            ? { status: progress.status, fraction: progress.fraction, stale: progress.stale, estimated: progress.estimated }
            : undefined
        }
      />
    );
  };
  const range = (from: number, to: number) => Array.from({ length: Math.max(0, to - from) }, (_, k) => node(from + k));

  const before = board;
  const after = calls.length - 1 - alight;
  return (
    <ol className={styles.timeline} style={{ "--line": lineColor(leg.routeColor) } as CSSProperties} aria-label="Stops">
      {before > 0 && (
        <StopsRollup
          side="before"
          count={before}
          expanded={state.before}
          onToggle={() => toggle("before")}
          passed={calls[board - 1]?.position === "passed"}
          trainHere={at >= 0 && at < board}
          anchor={calls[board]?.name ?? "boarding"}
        />
      )}
      {state.before && range(0, board)}
      {range(board, alight + 1)}
      {state.after && range(alight + 1, calls.length)}
      {after > 0 && (
        <StopsRollup
          side="after"
          count={after}
          expanded={state.after}
          onToggle={() => toggle("after")}
          passed={calls[alight + 1]?.position === "passed"}
          trainHere={at > alight}
          anchor={calls[alight]?.name ?? "alighting"}
        />
      )}
    </ol>
  );
}
