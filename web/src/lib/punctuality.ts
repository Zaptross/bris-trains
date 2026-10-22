import type { Punctuality } from "../api/types";

/** Semantic colour tone shared by atoms. */
export type Tone = "default" | "ok" | "warn" | "bad" | "info" | "muted";

/** Display properties for a punctuality value. */
export interface PunctualityInfo {
  glyph: string;
  label: string;
  tone: Tone;
}

/** Glyph, label and tone for a Punctuality, matching the CLI legend (SPEC §4.2). */
export function punctualityInfo(p: Punctuality, delaySeconds = 0): PunctualityInfo {
  const mins = Math.max(1, Math.round(Math.abs(delaySeconds) / 60));
  switch (p) {
    case "onTime":
      return { glyph: "\u25CF", label: "on time", tone: "ok" };
    case "late":
      return { glyph: "\u25B2", label: `${mins} min late`, tone: "warn" };
    case "veryLate":
      return { glyph: "\u25B2", label: `${mins} min late`, tone: "bad" };
    case "early":
      return { glyph: "\u25BC", label: `${mins} min early`, tone: "info" };
    case "scheduled":
      return { glyph: "\u25CB", label: "scheduled", tone: "muted" };
    case "skipping":
      return { glyph: "\u2715", label: "not stopping", tone: "bad" };
    case "cancelled":
      return { glyph: "\u2715", label: "cancelled", tone: "bad" };
  }
}
