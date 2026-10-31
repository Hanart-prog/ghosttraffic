import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { Bucket, Endpoint, Report } from "../api/types";
import { ms } from "../format";
import { Legend } from "./ui";

const BASELINE = "var(--baseline)";
const CANDIDATE = "var(--candidate)";

const tooltipStyle = {
  background: "var(--surface-raised)",
  border: "1px solid var(--rule-strong)",
  borderRadius: 8,
  color: "var(--text)",
};

// p95 for all endpoints on one shared axis, so scale cannot exaggerate noise.
export function LatencyComparison({ report }: { report: Report }) {
  const data = report.endpoints.map((endpoint) => ({
    name: endpoint.key,
    baseline: endpoint.baseline.response_time.p95_ms,
    candidate: endpoint.candidate.response_time.p95_ms,
  }));
  return (
    <figure>
      <Legend />
      <div style={{ width: "100%", height: 64 + data.length * 44 }} role="img" aria-label="p95 response time by endpoint, in the table below">
        <ResponsiveContainer>
          <BarChart data={data} layout="vertical" margin={{ left: 8, right: 24 }} barGap={2}>
            <CartesianGrid horizontal={false} stroke="var(--rule)" />
            <XAxis type="number" unit=" ms" stroke="var(--muted)" tick={{ fontSize: 12 }} />
            <YAxis type="category" dataKey="name" width={250} stroke="var(--muted)" tick={{ fontSize: 12 }} />
            <Tooltip contentStyle={tooltipStyle} formatter={(value) => ms(value as number)} cursor={{ fill: "var(--hover)" }} />
            <Bar dataKey="baseline" name="Baseline" fill={BASELINE} radius={[0, 4, 4, 0]} maxBarSize={14} isAnimationActive={false} />
            <Bar dataKey="candidate" name="Candidate" fill={CANDIDATE} radius={[0, 4, 4, 0]} maxBarSize={14} isAnimationActive={false} />
          </BarChart>
        </ResponsiveContainer>
      </div>
    </figure>
  );
}

// One dot per trial on a shared axis: the spread is what a single p95 hides.
export function TrialSpread({ endpoint }: { endpoint: Endpoint }) {
  const series = [
    { name: "Baseline", kind: "baseline", values: endpoint.trials.map((t) => ({ trial: t.trial, p95: t.baseline_p95_ms })) },
    { name: "Candidate", kind: "candidate", values: endpoint.trials.map((t) => ({ trial: t.trial, p95: t.candidate_p95_ms })) },
  ].map((s) => ({ ...s, values: s.values.filter((v): v is { trial: number; p95: number } => v.p95 !== null) }));
  const all = series.flatMap((s) => s.values.map((v) => v.p95));
  if (all.length === 0) return <p className="muted">No trial has enough data for a p95.</p>;
  const max = Math.max(...all);
  const left = (value: number) => `${(value / (max || 1)) * 100}%`;
  return (
    <figure>
      <Legend />
      {series.map((s) => {
        const sorted = s.values.map((v) => v.p95).sort((a, b) => a - b);
        const range = sorted.length ? `${ms(sorted[0]!)} to ${ms(sorted.at(-1)!)}` : "no data";
        return (
          <div className="spread-row" key={s.kind}>
            <span className="spread-label">{s.name}</span>
            <div className="spread-track">
              {s.values.map((v) => (
                <span
                  key={v.trial}
                  className={`spread-dot ${s.kind}`}
                  style={{ left: left(v.p95) }}
                  role="img"
                  aria-label={`${s.name} trial ${v.trial}: ${ms(v.p95)}`}
                  title={`${s.name} trial ${v.trial}: ${ms(v.p95)}`}
                />
              ))}
            </div>
            <span className="spread-range">{range}</span>
          </div>
        );
      })}
      <div className="spread-axis">
        <span>0 ms</span>
        <span>{ms(max)}</span>
      </div>
    </figure>
  );
}

function combine(baseline: Bucket[], candidate: Bucket[]) {
  const uppers = [...new Set([...baseline, ...candidate].map((b) => b.upper_ms))].sort((a, b) => a - b);
  const count = (buckets: Bucket[], upper: number) => buckets.find((b) => b.upper_ms === upper)?.count ?? 0;
  const rows = uppers.map((upper) => ({
    bucket: `≤ ${upper} ms`,
    baseline: count(baseline, upper),
    candidate: count(candidate, upper),
  }));
  // Fixed buckets far past the data only add empty space.
  const last = rows.findLastIndex((row) => row.baseline > 0 || row.candidate > 0);
  return rows.slice(0, last + 2);
}

export function Histogram({ endpoint }: { endpoint: Endpoint }) {
  const data = combine(endpoint.baseline.histogram, endpoint.candidate.histogram);
  return (
    <figure>
      <Legend />
      <div style={{ width: "100%", height: 280 }} role="img" aria-label={`Response time distribution for ${endpoint.key}`}>
        <ResponsiveContainer>
          <BarChart data={data} barGap={2} margin={{ left: 8, right: 8 }}>
            <CartesianGrid vertical={false} stroke="var(--rule)" />
            <XAxis dataKey="bucket" stroke="var(--muted)" tick={{ fontSize: 12 }} />
            <YAxis stroke="var(--muted)" tick={{ fontSize: 12 }} allowDecimals={false} />
            <Tooltip contentStyle={tooltipStyle} cursor={{ fill: "var(--hover)" }} />
            <Bar dataKey="baseline" name="Baseline" fill={BASELINE} radius={[4, 4, 0, 0]} maxBarSize={28} isAnimationActive={false} />
            <Bar dataKey="candidate" name="Candidate" fill={CANDIDATE} radius={[4, 4, 0, 0]} maxBarSize={28} isAnimationActive={false} />
          </BarChart>
        </ResponsiveContainer>
      </div>
    </figure>
  );
}
