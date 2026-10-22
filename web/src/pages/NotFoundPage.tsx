import { Link } from "react-router";
import { LiveFreshness } from "../components/organisms/LiveFreshness";
import { Shell } from "../components/templates/Shell";
import { useNow } from "../hooks/useNow";
import styles from "./Pages.module.css";

/** Fallback for unknown paths. */
export function NotFoundPage() {
  const now = useNow();
  return (
    <Shell freshness={<LiveFreshness now={now} />}>
      <p className={styles.hint}>Nothing here.</p>
      <Link to="/" className={styles.back}>
        ← Departures
      </Link>
    </Shell>
  );
}
