import type { CSSProperties } from "react";
import { lineColor } from "../../lib/color";
import styles from "./LineSwatch.module.css";

/** Props for LineSwatch. */
export interface LineSwatchProps {
  /** Route colour, with or without '#'. Omit to inherit --line from an ancestor. */
  color?: string;
  orientation?: "vertical" | "horizontal";
}

/** Route colour bar. */
export function LineSwatch({ color, orientation = "vertical" }: LineSwatchProps) {
  const style = color === undefined ? undefined : ({ "--line": lineColor(color) } as CSSProperties);
  return <span aria-hidden="true" className={`${styles.swatch} ${styles[orientation]}`} style={style} />;
}
