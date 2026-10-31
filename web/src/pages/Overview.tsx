import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { RunSummary } from "../api/types";
import { useApi } from "../api/useApi";
import { Empty, Show, VerdictBadge } from "../components/ui";
import { when, words } from "../format";

function Figure({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <div className="figure-value">{value.toLocaleString()}</div>
      <div className="figure-label">{label}</div>
    </div>
  );
}

function Figures({ runs }: { runs: RunSummary[] }) {
  const count = (verdict: RunSummary["verdict"]) => runs.filter((run) => run.verdict === verdict).length;
  return (
    <div className="figures">
      <Figure label="Runs on disk" value={runs.length} />
      <Figure label="Confirmed regressions" value={count("confirmed_regression")} />
      <Figure label="Clean comparisons" value={count("no_regression")} />
      <Figure label="Runs without a report" value={count(null)} />
    </div>
  );
}

function RunCard({ run }: { run: RunSummary }) {
  return (
    <article className={`run-card ${run.verdict}`}>
      <div>
        <VerdictBadge verdict={run.verdict} />
      </div>
      <h3>
        <Link to={`/runs/${encodeURIComponent(run.run_id)}`}>{run.run_id}</Link>
      </h3>
      <div className="run-card-meta">
        {run.requests.sent.toLocaleString()} requests · {words(run.status)}
        <br />
        {when(run.started_at)}
      </div>
    </article>
  );
}

function RunTable({ runs }: { runs: RunSummary[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th scope="col">Run</th>
            <th scope="col">Verdict</th>
            <th scope="col">Status</th>
            <th scope="col" className="num">Requests</th>
            <th scope="col">Started</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((run) => (
            <tr key={run.run_id}>
              <td>
                <Link to={`/runs/${encodeURIComponent(run.run_id)}`}>{run.run_id}</Link>
              </td>
              <td>
                <VerdictBadge verdict={run.verdict} />
              </td>
              <td>{words(run.status)}</td>
              <td className="num">{run.requests.sent}</td>
              <td>{when(run.started_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function Overview() {
  const runs = useApi((signal) => api.runs(signal), []);
  return (
    <>
      <section className="hero">
        <div>
          <span className="tag">Differential replay</span>
          <h1 className="display">
            Replay recorded traffic
            <span className="display-accent">Catch what changed</span>
          </h1>
        </div>
        <p className="lede">
          Every run sends one recorded trace to a baseline and a candidate, then compares latency and responses
          across repeated trials.
        </p>
      </section>
      <Show value={runs} what="runs">
        {(list) => {
          if (list.length === 0) {
            return (
              <Empty>
                <strong>No runs yet.</strong> Create one with <code>make regression-demo</code>, then reload this page.
              </Empty>
            );
          }
          const compared = list.filter((run) => run.verdict !== null);
          const replays = list.filter((run) => run.verdict === null);
          return (
            <>
              <Figures runs={list} />
              {compared.length > 0 && (
                <section className="section">
                  <span className="tag">Comparisons</span>
                  <h2>Runs with a verdict</h2>
                  <div className="run-grid">
                    {compared.map((run) => (
                      <RunCard key={run.run_id} run={run} />
                    ))}
                  </div>
                </section>
              )}
              {replays.length > 0 && (
                <section className="section">
                  <span className="tag">Replays</span>
                  <h2>Runs without a report</h2>
                  <RunTable runs={replays} />
                </section>
              )}
            </>
          );
        }}
      </Show>
    </>
  );
}
