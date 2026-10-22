import type { CSSProperties } from "react";
import type { Journey } from "../../api/types";
import { lineColor } from "../../lib/color";
import { Countdown } from "../atoms/Countdown";
import { DelayChip } from "../atoms/DelayChip";
import { LineSwatch } from "../atoms/LineSwatch";
import { PlatformChip } from "../atoms/PlatformChip";
import { StatusDot } from "../atoms/StatusDot";
import { TimeLabel } from "../atoms/TimeLabel";
import { ChangeSummary } from "./ChangeSummary";
import styles from "./JourneyRow.module.css";

/** Props for JourneyRow. */
export interface JourneyRowProps {
  journey: Journey;
  now: Date;
  onSelect?(): void;
  /** Realtime unavailable: mute the row and read "schedule only". */
  degraded?: boolean;
}

/** One departure-board row; a two-leg journey summarises its change inline. */
export function JourneyRow({ journey, now, onSelect, degraded }: JourneyRowProps) {
  const first = journey.legs[0];
  if (!first) return null;
  const board = first.calls[first.board];
  const cancelled = journey.punctuality === "cancelled";
  const runs = journey.legs.map((l) => l.runNumber ?? "—").join(" → ");
  const style = { "--line": lineColor(first.routeColor) } as CSSProperties;
  const cls = [styles.row, degraded && styles.degraded, cancelled && styles.cancelled].filter(Boolean).join(" ");
  return (
    <button type="button" className={cls} style={style} onClick={onSelect} disabled={!onSelect}>
      <span className={styles.swatches}>
        {journey.legs.map((l) => (
          <LineSwatch key={l.tripId} color={l.routeColor} />
        ))}
      </span>
      <span className={styles.when}>
        <span className={styles.depart}>
          <TimeLabel iso={journey.depart} size="lg" struck={cancelled} />
        </span>
        <Countdown iso={journey.depart} now={now} />
      </span>
      <span className={styles.body}>
        <span className={styles.top}>
          <PlatformChip platform={board?.platform ?? null} />
          {degraded ? (
            <span className={styles.schedOnly}>schedule only</span>
          ) : (
            <>
              <StatusDot punctuality={journey.punctuality} delaySeconds={first.delaySeconds} showLabel />
              {journey.punctuality === "onTime" && first.delaySeconds !== 0 && (
                <DelayChip punctuality="onTime" delaySeconds={first.delaySeconds} />
              )}
            </>
          )}
        </span>
        <span className={styles.route}>
          {first.routeName}
          <span className={styles.headsign}> → {first.headsign}</span>
        </span>
        {journey.change && <ChangeSummary change={journey.change} />}
        <span className={styles.meta}>
          <span>
            arr <TimeLabel iso={journey.arrive} size="sm" />
          </span>
          <span className={styles.run}>run {runs}</span>
        </span>
      </span>
    </button>
  );
}
