import { useState } from "react";
import { useNavigate } from "react-router";
import type { Station } from "../api/types";
import { useJourneys, useStations } from "../api/queries";
import { Spinner } from "../components/atoms/Spinner";
import { DepartureBoard } from "../components/organisms/DepartureBoard";
import { JourneySearchForm } from "../components/organisms/JourneySearchForm";
import { LiveFreshness } from "../components/organisms/LiveFreshness";
import { SearchTemplate } from "../components/templates/SearchTemplate";
import { useNow } from "../hooks/useNow";
import { loadLastPair } from "../lib/lastPair";
import { journeyHref } from "../lib/tripLink";
import styles from "./Pages.module.css";

function Departures({ stations, now }: { stations: Station[]; now: Date }) {
  const navigate = useNavigate();
  const [pair, setPair] = useState(() => loadLastPair(stations));
  const { origin, destination } = pair;
  const q = useJourneys(origin?.id ?? null, destination?.id ?? null);
  // keepPreviousData spans key changes; never show another pair's trains as this pair's.
  const data =
    q.data && q.data.origin.id === origin?.id && q.data.destination.id === destination?.id ? q.data : undefined;
  const ready = origin && destination && origin.id !== destination.id;

  return (
    <SearchTemplate
      freshness={<LiveFreshness source={data} now={now} />}
      degraded={data?.degraded}
      search={
        <JourneySearchForm
          stations={stations}
          origin={origin}
          destination={destination}
          onChange={(o, d) => setPair({ origin: o, destination: d })}
        />
      }
      board={
        ready ? (
          <>
            <h2 className={styles.boardTitle}>
              {origin.name.replace(/ station$/i, "")} → {destination.name.replace(/ station$/i, "")}
            </h2>
            <DepartureBoard
              journeys={data?.journeys}
              loading={q.isFetching || q.isPending}
              error={data ? null : q.error}
              degraded={data?.degraded ?? false}
              now={now}
              onSelect={(j) => navigate(journeyHref(j))}
            />
          </>
        ) : (
          <p className={styles.hint}>Choose where you’re leaving from and where you’re going.</p>
        )
      }
    />
  );
}

/** `/`: station pickers and the live departure board. */
export function DeparturesPage() {
  const stations = useStations();
  const now = useNow();
  if (stations.data) return <Departures stations={stations.data.stations} now={now} />;
  return (
    <SearchTemplate
      freshness={<LiveFreshness now={now} />}
      search={
        stations.isError ? (
          <p className={styles.error} role="alert">
            Couldn’t load stations: {stations.error.message}
          </p>
        ) : (
          <Spinner label="Loading stations" />
        )
      }
      board={null}
    />
  );
}
