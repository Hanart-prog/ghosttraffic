import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Endpoint, Result } from "../api/types";
import { useApi } from "../api/useApi";
import { Histogram } from "../components/Charts";
import { Empty, ErrorState, Loading, RunNav, Show, Stat, VerdictBadge } from "../components/ui";
import { ms, outcome, words } from "../format";

const PAGE = 50;

function Requests({ id, endpoint, trials }: { id: string; endpoint: string; trials: number }) {
  const [target, setTarget] = useState("");
  const [trial, setTrial] = useState("");
  const [cursor, setCursor] = useState<number | null>(null);
  const [earlier, setEarlier] = useState<(number | null)[]>([]);
  const page = useApi(
    (signal) => api.requests(id, { endpoint, target, trial, cursor, limit: PAGE }, signal),
    [id, endpoint, target, trial, cursor],
  );
  useEffect(() => {
    setCursor(null);
    setEarlier([]);
  }, [target, trial]);

  return (
    <>
      <div className="filters">
        <label>
          Target{" "}
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="">Both</option>
            <option value="baseline">Baseline</option>
            <option value="candidate">Candidate</option>
          </select>
        </label>
        <label>
          Trial{" "}
          <select value={trial} onChange={(e) => setTrial(e.target.value)}>
            <option value="">All</option>
            {Array.from({ length: trials }, (_, i) => (
              <option key={i + 1} value={i + 1}>
                {i + 1}
              </option>
            ))}
          </select>
        </label>
      </div>
      {page.state === "loading" && <Loading what="requests" />}
      {page.state === "error" && <ErrorState message={page.message} />}
      {page.state === "ready" &&
        (page.data.items.length === 0 ? (
          <Empty>No requests match these filters.</Empty>
        ) : (
          <>
            <RequestTable id={id} items={page.data.items} />
            <div className="pager">
              <button
                disabled={earlier.length === 0}
                onClick={() => {
                  setCursor(earlier.at(-1) ?? null);
                  setEarlier(earlier.slice(0, -1));
                }}
              >
                Previous
              </button>
              <button
                disabled={page.data.next_cursor === null}
                onClick={() => {
                  setEarlier([...earlier, cursor]);
                  setCursor(page.data.next_cursor);
                }}
              >
                Next
              </button>
            </div>
          </>
        ))}
    </>
  );
}

function RequestTable({ id, items }: { id: string; items: Result[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th scope="col">Request</th>
            <th scope="col">Target</th>
            <th scope="col" className="num">Trial</th>
            <th scope="col" className="num">Status</th>
            <th scope="col">Outcome</th>
            <th scope="col" className="num">Delay</th>
            <th scope="col" className="num">Service time</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={`${item.trial}-${item.target}-${item.request_id}`}>
              <td>
                <Link to={`/runs/${encodeURIComponent(id)}/requests/${encodeURIComponent(item.request_id)}?trial=${item.trial}`}>
                  {item.request_id}
                </Link>
              </td>
              <td>{words(item.target)}</td>
              <td className="num">{item.trial}</td>
              <td className="num">{item.status || "—"}</td>
              <td>{outcome(item.error_category)}</td>
              <td className="num">{ms(item.scheduling_delay_ms)}</td>
              <td className="num">{ms(item.duration_ms)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Detail({ id, endpoint }: { id: string; endpoint: Endpoint }) {
  const { baseline, candidate } = endpoint;
  return (
    <>
      <p className="verdict">
        <VerdictBadge verdict={endpoint.verdict} />
      </p>
      <div className="stats">
        <Stat label="p50 response" value={ms(candidate.response_time.p50_ms)} note={`baseline ${ms(baseline.response_time.p50_ms)}`} />
        <Stat label="p95 response" value={ms(candidate.response_time.p95_ms)} note={`baseline ${ms(baseline.response_time.p95_ms)}`} />
        <Stat label="p99 response" value={ms(candidate.response_time.p99_ms)} note={`baseline ${ms(baseline.response_time.p99_ms)}`} />
        <Stat label="Samples" value={String(candidate.response_time.samples)} note={`baseline ${baseline.response_time.samples}`} />
      </div>
      <h2>Response time distribution</h2>
      <Histogram endpoint={endpoint} />
      <details>
        <summary>Table view by trial</summary>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Trial</th>
                <th scope="col" className="num">Baseline p95</th>
                <th scope="col" className="num">Candidate p95</th>
              </tr>
            </thead>
            <tbody>
              {endpoint.trials.map((trial) => (
                <tr key={trial.trial}>
                  <td>{trial.trial}</td>
                  <td className="num">{ms(trial.baseline_p95_ms)}</td>
                  <td className="num">{ms(trial.candidate_p95_ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
      <h2>Requests</h2>
      <Requests id={id} endpoint={endpoint.key} trials={endpoint.trials.length} />
    </>
  );
}

export function EndpointDetail() {
  const { id = "", endpoint: key = "" } = useParams();
  const endpoints = useApi((signal) => api.endpoints(id, signal), [id]);
  return (
    <>
      <h1><code>{key}</code></h1>
      <RunNav />
      <p><Link to={`/runs/${encodeURIComponent(id)}/endpoints`}>All endpoints</Link></p>
      <Show value={endpoints} what="this endpoint">
        {(list) => {
          const endpoint = list.find((e) => e.key === key);
          return endpoint ? <Detail id={id} endpoint={endpoint} /> : <Empty>This run has no endpoint with that name.</Empty>;
        }}
      </Show>
    </>
  );
}
