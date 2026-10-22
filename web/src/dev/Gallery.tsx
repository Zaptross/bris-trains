import { useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import type { Punctuality, Station } from "../api/types";
import { Badge } from "../components/atoms/Badge";
import { Button } from "../components/atoms/Button";
import { Countdown } from "../components/atoms/Countdown";
import { DelayChip } from "../components/atoms/DelayChip";
import { Input } from "../components/atoms/Input";
import { LineSwatch } from "../components/atoms/LineSwatch";
import { PlatformChip } from "../components/atoms/PlatformChip";
import { Spinner } from "../components/atoms/Spinner";
import { StatusDot } from "../components/atoms/StatusDot";
import { Text } from "../components/atoms/Text";
import { TimeLabel } from "../components/atoms/TimeLabel";
import { AlertBanner } from "../components/molecules/AlertBanner";
import { ChangeSummary } from "../components/molecules/ChangeSummary";
import { FreshnessIndicator } from "../components/molecules/FreshnessIndicator";
import { JourneyRow } from "../components/molecules/JourneyRow";
import { StationPicker } from "../components/molecules/StationPicker";
import { TimelineNode } from "../components/molecules/TimelineNode";
import type { TimelineVehicle } from "../components/molecules/TimelineNode";
import { lineColor } from "../lib/color";
import { NOW, alerts, centralToRoma, fernyToCleveland, freshness, stations, t, timelineLeg } from "./fixtures";
import styles from "./Gallery.module.css";

const PUNCTUALITIES: [Punctuality, number][] = [
  ["onTime", 0],
  ["onTime", 34],
  ["late", 95],
  ["veryLate", 420],
  ["early", -75],
  ["scheduled", 0],
  ["skipping", 0],
  ["cancelled", 0],
];

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className={styles.section}>
      <Text as="h2" size="lg" strong>
        {title}
      </Text>
      <div className={styles.body}>{children}</div>
    </section>
  );
}

function Timeline({ vehicle, title }: { vehicle?: TimelineVehicle; title: string }) {
  const calls = timelineLeg.calls;
  return (
    <div className={styles.timeline} style={{ "--line": lineColor(timelineLeg.routeColor) } as CSSProperties}>
      <Text size="sm" tone="muted">
        {title}
      </Text>
      <ol className={styles.ol}>
        {calls.map((c, i) => (
          <TimelineNode
            key={c.stationId}
            call={c}
            isFirst={i === 0}
            isLast={i === calls.length - 1}
            isBoard={i === timelineLeg.board}
            isAlight={i === timelineLeg.alight}
            vehicle={c.position === "current" ? vehicle : undefined}
          />
        ))}
      </ol>
    </div>
  );
}

/** Dev gallery: every primitive in every state, from typed fixtures. */
export function Gallery() {
  const [from, setFrom] = useState<Station | null>(stations.find((s) => s.id === "place_censta") ?? null);
  const [to, setTo] = useState<Station | null>(null);
  const [selected, setSelected] = useState("");

  return (
    <main className={styles.gallery}>
      <header className={styles.header}>
        <Text as="h1" size="xl" strong>
          UI primitives
        </Text>
        <Text size="sm" tone="muted">
          now = <TimeLabel iso={NOW.toISOString()} size="sm" /> Fri 23 Oct (fixture)
        </Text>
      </header>

      <Section title="Text / Badge / Spinner / Button / Input">
        <div className={styles.wrap}>
          <Text>default</Text>
          <Text tone="muted">muted</Text>
          <Text tone="ok">ok</Text>
          <Text tone="warn">warn</Text>
          <Text tone="bad">bad</Text>
          <Text mono>mono 12:34</Text>
        </div>
        <div className={styles.wrap}>
          <Badge>default</Badge>
          <Badge tone="ok">ok</Badge>
          <Badge tone="warn">warn</Badge>
          <Badge tone="bad">✕ not stopping</Badge>
          <Badge tone="info">board</Badge>
          <Badge tone="muted">muted</Badge>
          <Spinner />
        </div>
        <div className={styles.wrap}>
          <Button>Primary</Button>
          <Button variant="ghost">⇅ Swap</Button>
          <Button disabled>Disabled</Button>
        </div>
        <Input placeholder="Plain input" aria-label="Plain input" />
      </Section>

      <Section title="TimeLabel / Countdown">
        <div className={styles.grid}>
          {["08:00:10", "08:03", "08:59", "09:05", "07:55"].map((hm) => (
            <div key={hm} className={styles.cell}>
              <TimeLabel iso={t(hm)} size="lg" />
              <Countdown iso={t(hm)} now={NOW} />
            </div>
          ))}
          <div className={styles.cell}>
            <TimeLabel iso={t("08:03")} struck />
            <TimeLabel iso={t("08:03")} size="sm" muted />
          </div>
        </div>
      </Section>

      <Section title="StatusDot / DelayChip">
        <ul className={styles.list}>
          {PUNCTUALITIES.map(([p, d]) => (
            <li key={`${p}${d}`} className={styles.wrap}>
              <StatusDot punctuality={p} delaySeconds={d} showLabel />
              <DelayChip punctuality={p} delaySeconds={d} />
              <Text size="sm" tone="muted">
                ({p}, {d}s)
              </Text>
            </li>
          ))}
        </ul>
      </Section>

      <Section title="PlatformChip / LineSwatch">
        <div className={styles.wrap}>
          <PlatformChip platform="2" />
          <PlatformChip platform="10" />
          <PlatformChip platform={null} />
          <PlatformChip platform="2" variant="inline" />
          <PlatformChip platform={null} variant="inline" />
          <LineSwatch color="#D53E45" orientation="horizontal" />
          <LineSwatch color="00A8E5" orientation="horizontal" />
          <LineSwatch color="#008752" orientation="horizontal" />
          <LineSwatch color="bogus" orientation="horizontal" />
        </div>
      </Section>

      <Section title="FreshnessIndicator">
        <div className={styles.col}>
          <FreshnessIndicator {...freshness.live} now={NOW} />
          <FreshnessIndicator {...freshness.stale} now={NOW} />
          <FreshnessIndicator {...freshness.degraded} now={NOW} />
        </div>
      </Section>

      <Section title="AlertBanner">
        <AlertBanner alerts={alerts} defaultOpen />
        <AlertBanner alerts={alerts.slice(0, 1)} />
        <AlertBanner
          alerts={[{ id: "x", header: "Run 1894 cancelled", description: "", effect: "NO_SERVICE", cause: "OTHER_CAUSE", activeUntil: null }]}
        />
      </Section>

      <Section title="StationPicker">
        <div className={styles.pickers}>
          <StationPicker label="From" stations={stations} value={from} onChange={setFrom} placeholder="e.g. central" />
          <StationPicker label="To" stations={stations} value={to} onChange={setTo} placeholder="try “indro” or “romma”" />
        </div>
        <Text size="sm" tone="muted">
          from: {from?.name ?? "—"} · to: {to?.name ?? "—"}
        </Text>
      </Section>

      <Section title="ChangeSummary">
        <div className={styles.col}>
          {fernyToCleveland.map((j) => j.change && <ChangeSummary key={j.legs[0]?.tripId} change={j.change} />)}
        </div>
      </Section>

      <Section title="JourneyRow — Central → Roma Street">
        <div className={styles.col}>
          {centralToRoma.map((j) => (
            <JourneyRow key={j.legs[0]?.tripId} journey={j} now={NOW} onSelect={() => setSelected(j.legs[0]?.tripId ?? "")} />
          ))}
        </div>
        <Text size="sm" tone="muted">
          selected: {selected || "—"}
        </Text>
      </Section>

      <Section title="JourneyRow — Ferny Grove → Cleveland (one change)">
        <div className={styles.col}>
          {fernyToCleveland.map((j) => (
            <JourneyRow key={j.legs[0]?.tripId} journey={j} now={NOW} onSelect={() => undefined} />
          ))}
        </div>
      </Section>

      <Section title="JourneyRow — degraded">
        <div className={styles.col}>
          {centralToRoma.slice(0, 2).map((j) => (
            <JourneyRow key={j.legs[0]?.tripId} journey={j} now={NOW} degraded />
          ))}
        </div>
      </Section>

      <Section title="TimelineNode">
        <div className={styles.timelines}>
          <Timeline title="IN_TRANSIT_TO Enoggera, 40% along" vehicle={{ status: "IN_TRANSIT_TO", fraction: 0.4 }} />
          <Timeline title="STOPPED_AT Enoggera" vehicle={{ status: "STOPPED_AT", fraction: 1 }} />
          <Timeline title="Stale vehicle fix" vehicle={{ status: "IN_TRANSIT_TO", fraction: 0.7, stale: true }} />
        </div>
      </Section>
    </main>
  );
}
