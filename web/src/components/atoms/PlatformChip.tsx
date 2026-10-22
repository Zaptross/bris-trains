import styles from "./PlatformChip.module.css";

/** Props for PlatformChip. */
export interface PlatformChipProps {
  platform: string | null;
  /**
   * "chip" (default) is a boxed label for the departure board; "inline" is
   * plain text that reads as part of a time line ("08:58 · Platform 1").
   */
  variant?: "chip" | "inline";
}

/** "Platform 2"; "Platform —" when the platform is unknown. */
export function PlatformChip({ platform, variant = "chip" }: PlatformChipProps) {
  const known = platform !== null && platform !== "";
  const cls = [styles[variant], known ? "" : styles.none].filter(Boolean).join(" ");
  return (
    <span className={cls} aria-label={known ? `platform ${platform}` : "platform unknown"}>
      Platform {known ? platform : "—"}
    </span>
  );
}
