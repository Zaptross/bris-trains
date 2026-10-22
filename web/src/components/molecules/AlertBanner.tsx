import { useId, useState } from "react";
import type { Alert } from "../../api/types";
import { formatHM } from "../../lib/time";
import styles from "./AlertBanner.module.css";

/** Props for AlertBanner. */
export interface AlertBannerProps {
  alerts: Alert[];
  defaultOpen?: boolean;
}

/** Collapsible list of service alerts; renders nothing when empty. */
export function AlertBanner({ alerts, defaultOpen = false }: AlertBannerProps) {
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  if (alerts.length === 0) return null;
  const severe = alerts.some((a) => a.effect === "NO_SERVICE");
  return (
    <section className={`${styles.banner} ${severe ? styles.severe : ""}`}>
      <button type="button" className={styles.toggle} aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
        <span aria-hidden="true">⚠</span>
        <span className={styles.summary}>
          {alerts.length === 1 ? alerts[0]?.header : `${alerts.length} service alerts`}
        </span>
        <span aria-hidden="true" className={styles.chev}>{open ? "▴" : "▾"}</span>
      </button>
      <ul id={id} className={styles.list} hidden={!open}>
        {alerts.map((a) => (
          <li key={a.id} className={styles.item}>
            {alerts.length > 1 && <strong className={styles.header}>{a.header}</strong>}
            {a.description ? <p className={styles.desc}>{a.description}</p> : alerts.length === 1 && <p className={styles.desc}>No further details.</p>}
            {a.activeUntil && <span className={styles.until}>until {formatHM(a.activeUntil)}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}
