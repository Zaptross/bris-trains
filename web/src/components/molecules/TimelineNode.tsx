import type { CSSProperties } from "react";
import type { Call, VehicleFix } from "../../api/types";
import { formatOffset, secondsBetween } from "../../lib/time";
import { Badge } from "../atoms/Badge";
import { PlatformChip } from "../atoms/PlatformChip";
import { TimeLabel } from "../atoms/TimeLabel";
import styles from "./TimelineNode.module.css";

/** Vehicle marker placement for the current node. */
export interface TimelineVehicle {
  status: VehicleFix["status"];
  /** 0..1 progress along the rail from the previous node to this one (IN_TRANSIT_TO / INCOMING_AT). */
  fraction: number;
  stale?: boolean;
  /** No fresh fix: position interpolated on time between surrounding calls. */
  estimated?: boolean;
}

/** Props for TimelineNode. */
export interface TimelineNodeProps {
  call: Call;
  isFirst: boolean;
  isLast: boolean;
  isBoard: boolean;
  isAlight: boolean;
  vehicle?: TimelineVehicle;
}

/** One stop on the trip timeline, rendered from Position × Source × skipped (SPEC §6.3). */
export function TimelineNode({ call, isFirst, isLast, isBoard, isAlight, vehicle }: TimelineNodeProps) {
  const { position, source, skipped } = call;
  const observed = position === "passed" && source === "observed" && call.actual !== null;
  const shown = observed && call.actual !== null ? call.actual : call.expected;
  const offset = secondsBetween(call.scheduled, shown);
  const differs = Math.abs(offset) >= 30;
  const estimated = position !== "passed" && (source === "projected" || source === "scheduled");
  const notObserved = position === "passed" && !observed;
  const unknown = call.name.startsWith("Unknown stop");

  const isCurrent = position === "current";
  const inTransit = isCurrent && vehicle !== undefined && vehicle.status !== "STOPPED_AT";
  const fraction = Math.min(1, Math.max(0, vehicle?.fraction ?? 1));
  const markerStyle = inTransit ? ({ "--f": fraction } as CSSProperties) : undefined;

  const cls = [
    styles.node,
    styles[position],
    skipped && styles.skipped,
    (isBoard || isAlight) && styles.endpoint,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <li className={cls} aria-current={isCurrent ? "step" : undefined}>
      <span className={styles.rail} aria-hidden="true">
        {!isFirst && <span className={styles.above} />}
        {!isLast && <span className={styles.below} />}
        <span className={styles.dot} />
        {isCurrent && vehicle && (
          <span
            className={[styles.marker, inTransit && styles.transit, vehicle?.stale && styles.stale, vehicle?.estimated && styles.guess]
              .filter(Boolean)
              .join(" ")}
            style={markerStyle}
          />
        )}
      </span>
      <span className={styles.content}>
        <span className={styles.head}>
          <span className={`${styles.name} ${unknown ? styles.unknown : ""}`}>{call.name}</span>
          <PlatformChip platform={call.platform} />
          {isBoard && <Badge tone="info">board</Badge>}
          {isAlight && <Badge tone="info">alight</Badge>}
          {skipped && <Badge tone="bad">✕ not stopping</Badge>}
          {isCurrent && vehicle?.stale && <Badge tone="warn">position stale</Badge>}
          {isCurrent && vehicle?.estimated && <Badge tone="muted">~ estimated position</Badge>}
        </span>
        {!skipped && (
          <span className={styles.times}>
            {observed || !differs ? (
              <TimeLabel iso={observed ? shown : call.scheduled} muted={notObserved} />
            ) : (
              <>
                <TimeLabel iso={call.scheduled} struck />
                <TimeLabel iso={call.expected} />
              </>
            )}
            {observed && differs && <TimeLabel iso={call.scheduled} size="sm" struck />}
            {differs && !notObserved && (
              <span className={`${styles.offset} ${offset > 0 ? (offset >= 300 ? styles.bad : offset >= 60 ? styles.warn : "") : styles.early}`}>
                {formatOffset(offset)}
              </span>
            )}
            {observed && <span className={styles.note}>actual</span>}
            {notObserved && <span className={styles.note}>not observed</span>}
            {estimated && (
              <span className={styles.estimated} title={source === "projected" ? "schedule + last known delay" : "timetable only"}>
                ~ estimated
              </span>
            )}
          </span>
        )}
      </span>
    </li>
  );
}
