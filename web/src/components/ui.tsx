import { useState, type ReactNode } from "react";
import { NavLink, useParams } from "react-router-dom";
import type { Loadable } from "../api/useApi";
import type { Verdict } from "../api/types";
import { words } from "../format";
import { chooseTheme, currentTheme } from "../theme";

export function Loading({ what }: { what: string }) {
  return (
    <p role="status" className="state">
      Loading {what}…
    </p>
  );
}

export function ErrorState({ message }: { message: string }) {
  return (
    <div role="alert" className="state error">
      <strong>That didn't load.</strong> {message}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="state">{children}</div>;
}

// Show renders the three states every page shares.
export function Show<T>({
  value,
  what,
  children,
}: {
  value: Loadable<T>;
  what: string;
  children: (data: T) => ReactNode;
}) {
  if (value.state === "loading") return <Loading what={what} />;
  if (value.state === "error") return <ErrorState message={value.message} />;
  return <>{children(value.data)}</>;
}

export const verdictIcons: Record<Verdict, string> = {
  confirmed_regression: "▲",
  observed_regression: "△",
  insufficient_samples: "?",
  no_regression: "✓",
};

// Color is backed by an icon and label, so state is never carried by hue alone.
export function VerdictBadge({ verdict }: { verdict: Verdict | null }) {
  if (verdict === null) return <span className="badge">No report</span>;
  return (
    <span className={`badge ${verdict}`}>
      <span aria-hidden="true">{verdictIcons[verdict]}</span> {words(verdict)}
    </span>
  );
}

export function Stat({ label, value, note, delta }: { label: string; value: string; note?: string; delta?: string }) {
  return (
    <div className="stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {note && (
        <div className="stat-note">
          {delta && <span className="stat-delta">{delta}</span>} {note}
        </div>
      )}
    </div>
  );
}

export function ThemeSwitch() {
  const [theme, setTheme] = useState(currentTheme);
  const next = theme === "dark" ? "light" : "dark";
  return (
    <button
      className="theme-switch"
      aria-label={`Switch to ${next} theme`}
      onClick={() => {
        chooseTheme(next);
        setTheme(next);
      }}
    >
      {words(next)}
    </button>
  );
}

export function GhostMark() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true">
      <path
        fill="currentColor"
        fillRule="evenodd"
        d="M10 1.5a7 7 0 0 0-7 7v9.25l2.33-1.75 2.34 1.75L10 16l2.33 1.75L14.67 16 17 17.75V8.5a7 7 0 0 0-7-7ZM7.25 7a1.25 1.25 0 1 0 0 2.5 1.25 1.25 0 0 0 0-2.5Zm5.5 0a1.25 1.25 0 1 0 0 2.5 1.25 1.25 0 0 0 0-2.5Z"
      />
    </svg>
  );
}

export function Legend() {
  return (
    <div className="legend">
      <span>
        <span className="key baseline" /> Baseline
      </span>
      <span>
        <span className="key candidate" /> Candidate
      </span>
    </div>
  );
}

export function RunNav() {
  const { id = "" } = useParams();
  const base = `/runs/${encodeURIComponent(id)}`;
  return (
    <nav aria-label="Run sections" className="tabs">
      <NavLink end to={base}>
        Summary
      </NavLink>
      <NavLink to={`${base}/endpoints`}>Endpoints</NavLink>
      <NavLink to={`${base}/timeline`}>Timeline</NavLink>
    </nav>
  );
}
