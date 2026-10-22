import { useId, useMemo, useState } from "react";
import type { KeyboardEvent } from "react";
import type { Station } from "../../api/types";
import { matchStations } from "../../lib/stationMatch";
import { Input } from "../atoms/Input";
import styles from "./StationPicker.module.css";

/** Props for StationPicker. */
export interface StationPickerProps {
  label: string;
  stations: Station[];
  value: Station | null;
  onChange(s: Station | null): void;
  placeholder?: string;
}

/** Accessible station combobox (ARIA 1.2 combobox + listbox) using the SPEC §3.7 matching tiers. */
export function StationPicker({ label, stations, value, onChange, placeholder }: StationPickerProps) {
  const id = useId();
  const listId = `${id}-list`;
  const [query, setQuery] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const text = query ?? value?.name ?? "";
  const matches = useMemo(() => (query === null ? [] : matchStations(query, stations)), [query, stations]);
  const expanded = open && matches.length > 0;
  const activeIdx = Math.min(active, matches.length - 1);

  function choose(s: Station) {
    onChange(s);
    setQuery(null);
    setOpen(false);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setOpen(true);
        setActive(expanded ? (activeIdx + 1) % matches.length : 0);
        break;
      case "ArrowUp":
        e.preventDefault();
        setOpen(true);
        setActive(expanded ? (activeIdx - 1 + matches.length) % matches.length : matches.length - 1);
        break;
      case "Enter": {
        const m = expanded ? matches[activeIdx] : undefined;
        if (m) {
          e.preventDefault();
          choose(m.station);
        }
        break;
      }
      case "Escape":
        if (expanded) {
          e.preventDefault();
          setOpen(false);
        } else if (query !== null) {
          setQuery(null);
        }
        break;
    }
  }

  return (
    <div className={styles.picker}>
      <label htmlFor={id} className={styles.label}>
        {label}
      </label>
      <Input
        id={id}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={expanded}
        aria-controls={listId}
        aria-activedescendant={expanded ? `${id}-opt-${activeIdx}` : undefined}
        autoComplete="off"
        spellCheck={false}
        enterKeyHint="search"
        placeholder={placeholder}
        value={text}
        onChange={(e) => {
          setQuery(e.target.value);
          setActive(0);
          setOpen(true);
          if (value !== null) onChange(null);
        }}
        onFocus={(e) => e.target.select()}
        onBlur={() => {
          setOpen(false);
          setQuery(null);
        }}
        onKeyDown={onKeyDown}
      />
      <ul id={listId} role="listbox" aria-label={`${label} suggestions`} className={styles.list} hidden={!expanded}>
        {matches.map((m, i) => (
          <li
            key={m.station.id}
            id={`${id}-opt-${i}`}
            role="option"
            aria-selected={i === activeIdx}
            className={`${styles.option} ${i === activeIdx ? styles.active : ""}`}
            onMouseDown={(e) => e.preventDefault()}
            onMouseMove={() => setActive(i)}
            onClick={() => choose(m.station)}
          >
            <span>{m.station.name}</span>
            {m.alias !== undefined && <span className={styles.alias}>“{m.alias}”</span>}
          </li>
        ))}
      </ul>
    </div>
  );
}
