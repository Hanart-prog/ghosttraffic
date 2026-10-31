import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Report, TargetStats } from "../api/types";
import { useApi } from "../api/useApi";
import { LatencyComparison } from "../components/Charts";
import { NoReport, RunNav, Show, Stat, verdictIcons } from "../components/ui";
import { change, ms, when, words } from "../format";

function errorRate(stats: TargetStats): string {
  if (stats.total === 0) return "n/a";
  return `${((stats.errors / stats.total) * 100).toFixed(2)}%`;
}

// Equal failures and uncaptured bodies are not differences between the targets.
// Warnings cover both, so nothing is hidden by leaving them out here.
const notDifferences = ["both_failed", "body_unavailable"];

// The worst endpoint at the run's own verdict, so the headline matches the report.
function subject(report: Report): string {
  if (report.verdict === "no_regression") return `across ${report.endpoints.length} endpoints`;
  if (report.verdict === "insufficient_samples") return "too few samples to judge";
  const ratios = report.endpoints
    .filter((e) => e.verdict === report.verdict)
    .flatMap((e) => {
      const [base, cand] = [e.baseline.response_time.p95_ms, e.candidate.response_time.p95_ms];
      return base && cand ? [{ key: e.key, ratio: cand / base }] : [];
    })
    .sort((a, b) => b.ratio - a.ratio);
  const worst = ratios[0];
  return worst ? `${worst.key}, ${worst.ratio.toFixed(1)}× baseline p95` : "";
}

function Rules({ report }: { report: Report }) {
  const { thresholds, provenance } = report;
  return (
    <pre className="spec" aria-label="Verdict rules for this report">
      <b>min_ratio</b>     {thresholds.min_ratio}{"\n"}
      <b>min_delta_ms</b>  {thresholds.min_delta_ms}{"\n"}
      <b>min_samples</b>   {thresholds.min_samples}{"\n"}
      <b>trials</b>        {provenance.trials}{"\n"}
      <b>trace</b>         {provenance.trace_sha256.slice(0, 12)}
    </pre>
  );
}

function Summary({ report }: { report: Report }) {
  const { baseline, candidate } = report.summary;
  const differences = report.differences.filter((d) => !notDifferences.includes(d.classification));
  const { thresholds } = report;
  return (
    <>
      <p className="muted">
        A regression needs p95 at least {thresholds.min_ratio}× baseline and {thresholds.min_delta_ms} ms slower, with{" "}
        {thresholds.min_samples}+ samples per trial. This is evidence of a difference, not of its cause.
      </p>
      <div className="stats">
        <Stat label="p50 response" value={ms(candidate.response_time.p50_ms)} delta={change(candidate.response_time.p50_ms, baseline.response_time.p50_ms)} note={`baseline ${ms(baseline.response_time.p50_ms)}`} />
        <Stat label="p95 response" value={ms(candidate.response_time.p95_ms)} delta={change(candidate.response_time.p95_ms, baseline.response_time.p95_ms)} note={`baseline ${ms(baseline.response_time.p95_ms)}`} />
        <Stat label="p99 response" value={ms(candidate.response_time.p99_ms)} delta={change(candidate.response_time.p99_ms, baseline.response_time.p99_ms)} note={`baseline ${ms(baseline.response_time.p99_ms)}`} />
        <Stat label="Throughput" value={`${candidate.throughput_rps.toFixed(1)} req/s`} note={`baseline ${baseline.throughput_rps.toFixed(1)}, wall-clock`} />
        <Stat label="Error rate" value={errorRate(candidate)} note={`baseline ${errorRate(baseline)}`} />
      </div>
      <section className="section">
        <span className="tag">Latency</span>
        <h2>p95 response time by endpoint</h2>
        <LatencyComparison report={report} />
      </section>
      {differences.length > 0 && (
        <section className="section">
          <span className="tag">Responses</span>
          <h2>Response differences</h2>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th scope="col">Request</th>
                  <th scope="col" className="num">Trial</th>
                  <th scope="col">Endpoint</th>
                  <th scope="col">Outcome</th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {differences.map((d) => (
                  <tr key={`${d.trial}-${d.request_id}`}>
                    <td>
                      <Link to={`requests/${encodeURIComponent(d.request_id)}?trial=${d.trial}`}>{d.request_id}</Link>
                    </td>
                    <td className="num">{d.trial}</td>
                    <td><code>{d.endpoint}</code></td>
                    <td>{words(d.classification)}</td>
                    <td>{d.baseline_status} / {d.candidate_status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {report.warnings.length > 0 && (
        <section className="section">
          <span className="tag">Notes</span>
          <h2>Warnings</h2>
          <ul className="warnings">
            {report.warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}

export function RunDetail() {
  const { id = "" } = useParams();
  const detail = useApi((signal) => api.run(id, signal), [id]);
  return (
    <Show value={detail} what="this run">
      {({ run, report, report_error }) => (
          <>
            <section className="hero">
              <div>
                <span className="tag">{id}</span>
                {report ? (
                  <>
                    <h1 className="display">
                      <span className={`verdict-icon ${report.verdict}`} aria-hidden="true">{verdictIcons[report.verdict]}</span>
                      {words(report.verdict)}
                      <span className="display-soft">{subject(report)}</span>
                    </h1>
                    <p className="verdict muted">
                      Across {report.provenance.trials} {report.provenance.trials === 1 ? "trial" : "trials"}
                    </p>
                  </>
                ) : (
                  <h1 className="display">{id}</h1>
                )}
              </div>
              {report && <Rules report={report} />}
            </section>
            <p className="muted">
              {run.baseline} against {run.candidate}. {words(run.status)}, started {when(run.started_at)}. {run.requests.sent} requests sent per pass
              {run.requests.skipped > 0 && `, ${run.requests.skipped} unreplayable`}.
            </p>
            <RunNav />
            {report ? (
              <Summary report={report} />
            ) : (
              <NoReport id={id} problem={report_error} />
            )}
          </>
      )}
    </Show>
  );
}
