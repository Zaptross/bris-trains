import { Link, useParams, useSearchParams } from "react-router";
import { ApiError } from "../api/client";
import { useLeg, useShape, useStations } from "../api/queries";
import { Spinner } from "../components/atoms/Spinner";
import { LineDiagram } from "../components/organisms/LineDiagram";
import { LiveFreshness } from "../components/organisms/LiveFreshness";
import { TripHeader } from "../components/organisms/TripHeader";
import { TripTimeline } from "../components/organisms/TripTimeline";
import { Shell } from "../components/templates/Shell";
import { TripTemplate } from "../components/templates/TripTemplate";
import { useNow } from "../hooks/useNow";
import { legProgress } from "../lib/progress";
import { parseTripLocation, tripHref } from "../lib/tripLink";
import styles from "./Pages.module.css";

/** `/trip/:tripId?date=&from=&to=[&leg=&j=…]`: header, stop timeline and line diagram for one leg. */
export function TripPage() {
  const { tripId = "" } = useParams();
  const [search] = useSearchParams();
  const loc = parseTripLocation(tripId, search);
  const now = useNow();
  const q = useLeg(loc.current);
  const data = q.data && q.data.leg.tripId === tripId ? q.data : undefined;
  const leg = data?.leg;
  const stations = useStations();
  const shape = useShape(leg?.shapeId ?? null);

  const back = (
    <Link to="/" className={styles.back}>
      ← Departures
    </Link>
  );

  if (!leg) {
    const notFound = q.error instanceof ApiError && q.error.status === 404;
    return (
      <Shell freshness={<LiveFreshness now={now} />}>
        {back}
        {q.isError ? (
          <p className={styles.error} role="alert">
            {notFound ? "This trip isn’t running on that date." : `Couldn’t load this trip: ${q.error.message}`}
          </p>
        ) : (
          <Spinner label="Loading trip" />
        )}
      </Shell>
    );
  }

  const progress = legProgress(leg, now);
  const switcher =
    loc.legs.length > 1 ? (
      <nav className={styles.legs} aria-label="Journey legs">
        {loc.legs.map((l, i) => (
          <Link
            key={l.tripId}
            to={tripHref(loc.legs, i)}
            replace
            className={styles.legTab}
            aria-current={i === loc.index ? "page" : undefined}
          >
            {i === 0 ? "1st train" : "2nd train"}
          </Link>
        ))}
      </nav>
    ) : null;

  return (
    <TripTemplate
      freshness={<LiveFreshness source={data} now={now} />}
      degraded={data.degraded}
      header={
        <>
          {back}
          <TripHeader leg={leg} degraded={data.degraded}>
            {switcher}
          </TripHeader>
        </>
      }
      timeline={<TripTimeline leg={leg} progress={progress} />}
      diagram={
        leg.shapeId === null ? undefined : shape.data && stations.data ? (
          <LineDiagram leg={leg} shape={shape.data} stations={stations.data.stations} progress={progress} />
        ) : shape.isError ? (
          <p className={styles.hint}>Line diagram unavailable.</p>
        ) : (
          <Spinner label="Loading line diagram" />
        )
      }
    />
  );
}
