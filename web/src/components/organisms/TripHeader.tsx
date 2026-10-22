import type { CSSProperties, ReactNode } from "react";
import type { Leg } from "../../api/types";
import { lineColor } from "../../lib/color";
import { DelayChip } from "../atoms/DelayChip";
import { LineSwatch } from "../atoms/LineSwatch";
import { StatusDot } from "../atoms/StatusDot";
import { AlertBanner } from "../molecules/AlertBanner";
import styles from "./TripHeader.module.css";

/** Props for TripHeader. */
export interface TripHeaderProps {
  leg: Leg;
  degraded?: boolean;
  /** Extra controls under the title, e.g. leg switcher. */
  children?: ReactNode;
}

/** Trip title block: route, headsign, run number, punctuality and alerts. */
export function TripHeader({ leg, degraded, children }: TripHeaderProps) {
  return (
    <header className={styles.header} style={{ "--line": lineColor(leg.routeColor) } as CSSProperties}>
      <div className={styles.title}>
        <LineSwatch />
        <div className={styles.text}>
          <h1 className={styles.route}>{leg.routeName}</h1>
          <p className={styles.sub}>
            → {leg.headsign.replace(/ station$/i, "")}
            <span className={styles.run}>run {leg.runNumber ?? "—"}</span>
          </p>
          <p className={styles.status}>
            {degraded ? (
              <span className={styles.sched}>schedule only</span>
            ) : (
              <>
                <StatusDot punctuality={leg.punctuality} delaySeconds={leg.delaySeconds} showLabel />
                {leg.punctuality === "onTime" && Math.abs(leg.delaySeconds) >= 30 && (
                  <DelayChip punctuality="onTime" delaySeconds={leg.delaySeconds} />
                )}
              </>
            )}
          </p>
        </div>
      </div>
      {children}
      <AlertBanner alerts={leg.alerts} />
    </header>
  );
}
