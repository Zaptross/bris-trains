/** A projected point in diagram units. */
export interface Pt {
  x: number;
  y: number;
}

/** Equirectangular projection about a centroid: x = (lon − lon0)·cos(lat0), y = −(lat − lat0). */
export function equirect(points: readonly (readonly [number, number])[]): (lat: number, lon: number) => Pt {
  let lat0 = 0;
  let lon0 = 0;
  for (const [la, lo] of points) {
    lat0 += la;
    lon0 += lo;
  }
  const n = Math.max(1, points.length);
  lat0 /= n;
  lon0 /= n;
  const k = Math.cos((lat0 * Math.PI) / 180);
  return (lat, lon) => ({ x: (lon - lon0) * k, y: -(lat - lat0) });
}

/** A polyline with cumulative arc lengths. */
export interface Polyline {
  pts: Pt[];
  cum: number[];
  total: number;
}

/** Builds a polyline with cumulative lengths. */
export function polyline(pts: Pt[]): Polyline {
  const cum = [0];
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1];
    const b = pts[i];
    cum.push((cum[i - 1] ?? 0) + (a && b ? Math.hypot(b.x - a.x, b.y - a.y) : 0));
  }
  return { pts, cum, total: cum[cum.length - 1] ?? 0 };
}

/** Arc-length distance of the point on the polyline nearest to p. */
export function locate(pl: Polyline, p: Pt): number {
  let best = Infinity;
  let at = 0;
  for (let i = 1; i < pl.pts.length; i++) {
    const a = pl.pts[i - 1];
    const b = pl.pts[i];
    if (!a || !b) continue;
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const len2 = dx * dx + dy * dy;
    const t = len2 === 0 ? 0 : Math.min(1, Math.max(0, ((p.x - a.x) * dx + (p.y - a.y) * dy) / len2));
    const d = Math.hypot(a.x + t * dx - p.x, a.y + t * dy - p.y);
    if (d < best) {
      best = d;
      at = (pl.cum[i - 1] ?? 0) + t * Math.sqrt(len2);
    }
  }
  return at;
}

/** Point at arc-length distance d along the polyline (clamped). */
export function pointAt(pl: Polyline, d: number): Pt {
  const first = pl.pts[0] ?? { x: 0, y: 0 };
  if (d <= 0 || pl.pts.length < 2) return first;
  for (let i = 1; i < pl.pts.length; i++) {
    const c1 = pl.cum[i] ?? 0;
    if (d <= c1) {
      const a = pl.pts[i - 1] ?? first;
      const b = pl.pts[i] ?? first;
      const c0 = pl.cum[i - 1] ?? 0;
      const t = c1 === c0 ? 0 : (d - c0) / (c1 - c0);
      return { x: a.x + t * (b.x - a.x), y: a.y + t * (b.y - a.y) };
    }
  }
  return pl.pts[pl.pts.length - 1] ?? first;
}
