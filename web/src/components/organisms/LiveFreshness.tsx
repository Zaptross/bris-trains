import type { JourneysResponse } from "../../api/types";
import { useHealth } from "../../api/queries";
import { Spinner } from "../atoms/Spinner";
import { FreshnessIndicator } from "../molecules/FreshnessIndicator";

/** Freshness fields carried by journeys and leg responses. */
export type FreshnessSource = Pick<JourneysResponse, "generatedAt" | "dataAgeSeconds" | "degraded">;

/** Props for LiveFreshness. */
export interface LiveFreshnessProps {
  /** Freshness from the page's primary response; falls back to /api/healthz when absent. */
  source?: FreshnessSource;
  now: Date;
}

/** Always-visible freshness: from the page's data, or feed health before any data exists. */
export function LiveFreshness({ source, now }: LiveFreshnessProps) {
  const health = useHealth();
  if (source) return <FreshnessIndicator {...source} now={now} />;
  const h = health.data;
  if (h) {
    return (
      <FreshnessIndicator
        generatedAt={new Date(health.dataUpdatedAt).toISOString()}
        dataAgeSeconds={h.tripUpdates.ageSeconds ?? 0}
        degraded={h.degraded || h.tripUpdates.ageSeconds === null}
        now={now}
      />
    );
  }
  if (health.isError) return <FreshnessIndicator generatedAt="" dataAgeSeconds={0} degraded now={now} />;
  return <Spinner label="Checking realtime feed" />;
}
