import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Report, TargetStats } from "../api/types";
import { useApi } from "../api/useApi";
import { LatencyComparison } from "../components/Charts";
import { Empty, RunNav, Show, Stat, VerdictBadge } from "../components/ui";
import { ms, when, words } from "../format";

function errorRate(stats: TargetStats): string {
  if (stats.total === 0) return "n/a";
  return `${((stats.errors / stats.total) * 100).toFixed(2)}%`;
}

function Summary({ report }: { report: Report }) {
  const { baseline, candidate } = report.summary;
  const { thresholds } = report;
  return (
    <>
      <p className="verdict">
        <VerdictBadge verdict={report.verdict} /> across {report.provenance.trials} trial(s)
      </p>
      <p className="muted">
        A regression needs p95 at least {thresholds.min_ratio}× baseline and {thresholds.min_delta_ms} ms slower, with{" "}
        {thresholds.min_samples}+ samples per trial. This is evidence of a difference, not of its cause.
      </p>
      <div className="stats">
        <Stat label="p50 response" value={ms(candidate.response_time.p50_ms)} note={`baseline ${ms(baseline.response_time.p50_ms)}`} />
        <Stat label="p95 response" value={ms(candidate.response_time.p95_ms)} note={`baseline ${ms(baseline.response_time.p95_ms)}`} />
        <Stat label="p99 response" value={ms(candidate.response_time.p99_ms)} note={`baseline ${ms(baseline.response_time.p99_ms)}`} />
        <Stat label="Throughput" value={`${candidate.throughput_rps.toFixed(1)} req/s`} note={`baseline ${baseline.throughput_rps.toFixed(1)}, wall-clock`} />
        <Stat label="Error rate" value={errorRate(candidate)} note={`baseline ${errorRate(baseline)}`} />
      </div>
      <h2>p95 response time by endpoint</h2>
      <LatencyComparison report={report} />
      {report.differences.length > 0 && (
        <>
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
                {report.differences.map((d) => (
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
        </>
      )}
      {report.warnings.length > 0 && (
        <>
          <h2>Warnings</h2>
          <ul className="warnings">
            {report.warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </>
      )}
    </>
  );
}

export function RunDetail() {
  const { id = "" } = useParams();
  const detail = useApi((signal) => api.run(id, signal), [id]);
  return (
    <>
      <h1>{id}</h1>
      <Show value={detail} what="this run">
        {({ run, report, report_error }) => (
          <>
            <p className="muted">
              {run.baseline} against {run.candidate}. {words(run.status)}, started {when(run.started_at)}. {run.requests.sent} requests sent per pass
              {run.requests.skipped > 0 && `, ${run.requests.skipped} unreplayable`}.
            </p>
            <RunNav />
            {report ? (
              <Summary report={report} />
            ) : (
              <Empty>
                <strong>{report_error ?? "This run has no report yet."}</strong> Build it with{" "}
                <code>ghosttraffic report --run runs/{id}</code>.
              </Empty>
            )}
          </>
        )}
      </Show>
    </>
  );
}
