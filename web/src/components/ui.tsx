import type { ReactNode } from "react";
import { NavLink, useParams } from "react-router-dom";
import type { Loadable } from "../api/useApi";
import type { Verdict } from "../api/types";
import { words } from "../format";

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

const verdictIcons: Record<Verdict, string> = {
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

export function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {note && <div className="stat-note">{note}</div>}
    </div>
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
