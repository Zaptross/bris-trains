import type { ComponentProps } from "react";
import styles from "./Input.module.css";

/** Props for Input: native input props (ref supported). */
export type InputProps = ComponentProps<"input">;

/** Touch-sized text input. */
export function Input({ className, ...rest }: InputProps) {
  return <input className={[styles.input, className].filter(Boolean).join(" ")} {...rest} />;
}
