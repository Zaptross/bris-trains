import type { ReactNode } from "react";
import type { Tone } from "../../lib/punctuality";
import styles from "./Text.module.css";

/** Props for Text. */
export interface TextProps {
  as?: "span" | "p" | "h1" | "h2" | "h3" | "div";
  size?: "sm" | "md" | "lg" | "xl";
  tone?: Tone;
  mono?: boolean;
  strong?: boolean;
  className?: string;
  children: ReactNode;
}

/** Typographic primitive. */
export function Text({ as: Tag = "span", size = "md", tone = "default", mono, strong, className, children }: TextProps) {
  const cls = [styles.text, styles[size], styles[tone], mono && styles.mono, strong && styles.strong, className]
    .filter(Boolean)
    .join(" ");
  return <Tag className={cls}>{children}</Tag>;
}
