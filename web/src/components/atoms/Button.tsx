import type { ComponentProps } from "react";
import styles from "./Button.module.css";

/** Props for Button: native button props plus a visual variant. */
export type ButtonProps = ComponentProps<"button"> & { variant?: "primary" | "ghost" };

/** Touch-sized button (≥ 44 px). */
export function Button({ variant = "primary", className, type = "button", ...rest }: ButtonProps) {
  return <button type={type} className={[styles.button, styles[variant], className].filter(Boolean).join(" ")} {...rest} />;
}
