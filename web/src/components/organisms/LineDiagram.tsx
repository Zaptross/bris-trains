import { useMemo } from "react";
import type { CSSProperties } from "react";
import type { Leg, ShapeResponse, Station } from "../../api/types";
import { lineColor } from "../../lib/color";
import { equirect, locate, pointAt, polyline } from "../../lib/geo";
import type { Pt } from "../../lib/geo";
import { freshFix } from "../../lib/progress";
import type { LegProgress } from "../../lib/progress";
import styles from "./LineDiagram.module.css";

/** Props for LineDiagram. */
export interface LineDiagramProps {
  leg: Leg;
  shape: ShapeResponse;
  stations: Station[];
  progress: LegProgress | null;
}

const SIZE = 1000;
const PAD = 60;

/** Inline SVG of the trip's shape with station nodes and the train position. No tiles, no basemap. */
export function LineDiagram({ leg, shape, stations, progress }: LineDiagramProps) {
  const geom = useMemo(() => {
    const project = equirect(shape.points);
    const raw = shape.points.map(([la, lo]) => project(la, lo));
    const xs = raw.map((p) => p.x);
    const ys = raw.map((p) => p.y);
    const minX = Math.min(...xs);
    const minY = Math.min(...ys);
    const span = Math.max(Math.max(...xs) - minX, Math.max(...ys) - minY, 1e-9);
    const s = SIZE / span;
    const fit = (p: Pt): Pt => ({ x: (p.x - minX) * s + PAD, y: (p.y - minY) * s + PAD });
    const width = (Math.max(...xs) - minX) * s + 2 * PAD;
    const height = (Math.max(...ys) - minY) * s + 2 * PAD;
    const line = polyline(raw.map(fit));
    const byId = new Map(stations.map((st) => [st.id, st]));
    const nodes = leg.calls.map((c) => {
      const st = byId.get(c.stationId);
      if (!st) return null;
      const d = locate(line, fit(project(st.lat, st.lon)));
      return { d, p: pointAt(line, d) };
    });
    return { project, fit, line, nodes, width, height };
  }, [shape, stations, leg.calls]);

  const fix = freshFix(leg);
  let vehicle: { p: Pt; d: number; estimated: boolean } | null = null;
  if (fix) {
    const d = locate(geom.line, geom.fit(geom.project(fix.lat, fix.lon)));
    vehicle = { p: pointAt(geom.line, d), d, estimated: false };
  } else if (progress) {
    const cur = geom.nodes[progress.index];
    const prev = progress.prevIndex === null ? null : geom.nodes[progress.prevIndex];
    if (cur) {
      const d = prev ? prev.d + (cur.d - prev.d) * progress.fraction : cur.d;
      vehicle = { p: pointAt(geom.line, d), d, estimated: true };
    }
  }

  const path = geom.line.pts.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ");
  const travelled = vehicle
    ? [...geom.line.pts.filter((_, i) => (geom.line.cum[i] ?? 0) < (vehicle?.d ?? 0)), vehicle.p]
        .map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`)
        .join(" ")
    : "";
  const cx = geom.width / 2;
  const boardNode = geom.nodes[leg.board];
  const label = (i: number, text: string, avoid?: Pt) => {
    const n = geom.nodes[i];
    if (!n) return null;
    const right = n.p.x < cx;
    // Nudge the second label vertically when it would collide with the first (e.g. Central / Roma Street).
    const clash = avoid && Math.abs(avoid.y - n.p.y) < 40 && Math.abs(avoid.x - n.p.x) < 260;
    const dy = clash ? (n.p.y > geom.height / 2 ? -40 : 40) : 0;
    return (
      <text x={n.p.x + (right ? 22 : -22)} y={n.p.y + 9 + dy} textAnchor={right ? "start" : "end"} className={styles.label}>
        {text.replace(/ station$/i, "")}
      </text>
    );
  };

  return (
    <figure className={styles.figure} style={{ "--line": lineColor(leg.routeColor) } as CSSProperties}>
      <svg
        viewBox={`0 0 ${geom.width.toFixed(0)} ${geom.height.toFixed(0)}`}
        className={styles.svg}
        role="img"
        aria-label={`Route diagram for ${leg.routeName}${vehicle ? (vehicle.estimated ? ", estimated train position" : ", live train position") : ""}`}
      >
        <polyline points={path} className={styles.route} />
        {travelled && <polyline points={travelled} className={styles.done} />}
        {leg.calls.map((c, i) => {
          const n = geom.nodes[i];
          if (!n) return null;
          const end = i === leg.board || i === leg.alight;
          const cls = [styles.node, c.position === "passed" && styles.passed, c.skipped && styles.skipped, end && styles.end]
            .filter(Boolean)
            .join(" ");
          return <circle key={`${i}-${c.stationId}`} cx={n.p.x} cy={n.p.y} r={end ? 14 : 9} className={cls} />;
        })}
        {label(leg.board, leg.calls[leg.board]?.name ?? "")}
        {leg.alight !== leg.board && label(leg.alight, leg.calls[leg.alight]?.name ?? "", boardNode?.p)}
        {vehicle && (
          <g className={vehicle.estimated ? styles.estimated : styles.vehicle}>
            <circle cx={vehicle.p.x} cy={vehicle.p.y} r={22} className={styles.halo} />
            <circle cx={vehicle.p.x} cy={vehicle.p.y} r={13} className={styles.train} />
          </g>
        )}
      </svg>
      <figcaption className={styles.caption}>
        {vehicle ? (vehicle.estimated ? "○ estimated position (no live fix)" : "● live position") : "no current position"}
      </figcaption>
    </figure>
  );
}
