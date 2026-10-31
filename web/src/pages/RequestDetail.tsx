import { Link, useParams, useSearchParams } from "react-router-dom";
import { api } from "../api/client";
import type { Result } from "../api/types";
import { useApi } from "../api/useApi";
import { Empty, RunNav, Show } from "../components/ui";
import { ms, outcome, words } from "../format";

function Attempt({ title, result }: { title: string; result: Result | null }) {
  if (!result) {
    return (
      <section className="attempt">
        <h2>{title}</h2>
        <Empty>No result for this target.</Empty>
      </section>
    );
  }
  return (
    <section className="attempt">
      <h2>{title}</h2>
      <dl>
        <dt>Status</dt>
        <dd>{result.status || "None"}</dd>
        <dt>Outcome</dt>
        <dd>{outcome(result.error_category)}</dd>
        <dt>Service time</dt>
        <dd>{ms(result.duration_ms)}</dd>
        <dt>Scheduling delay</dt>
        <dd>{ms(result.scheduling_delay_ms)}</dd>
        <dt>Waiting for earlier step</dt>
        <dd>{ms(result.dependency_wait_ms ?? 0)}</dd>
      </dl>
      <h3>Response</h3>
      {result.response_json === undefined ? (
        <p className="muted">Not available. Bodies are stored only when a run uses --capture-bodies.</p>
      ) : (
        <pre>{JSON.stringify(result.response_json, null, 2)}</pre>
      )}
    </section>
  );
}

export function RequestDetail() {
  const { id = "", request = "" } = useParams();
  const [search] = useSearchParams();
  const trial = search.get("trial") ?? "1";
  const detail = useApi((signal) => api.request(id, request, trial, signal), [id, request, trial]);
  return (
    <>
      <h1>{request}</h1>
      <RunNav />
      <p className="muted">Trial {trial}</p>
      <Show value={detail} what="this request">
        {(data) => (
          <>
            {data.classification && (
              <p className="verdict">
                Comparison: <strong>{words(data.classification)}</strong>
              </p>
            )}
            {data.differing_paths.length > 0 && (
              <>
                <h2>Differences</h2>
                <ul>
                  {data.differing_paths.map((path) => (
                    <li key={path}><code>{path}</code></li>
                  ))}
                </ul>
              </>
            )}
            {data.ignored_paths.length > 0 && (
              <p className="muted">
                Ignored in comparison: {data.ignored_paths.map((path) => <code key={path}>{path} </code>)}
              </p>
            )}
            <h2>Request</h2>
            {data.request ? (
              <>
                <p><code>{data.request.method} {data.request.path}</code></p>
                <p className="muted">As recorded and sanitized. Workflows may substitute generated IDs when sending.</p>
                {data.request.body_json !== null && <pre>{JSON.stringify(data.request.body_json, null, 2)}</pre>}
              </>
            ) : (
              <p className="muted">Not available: this run predates stored requests.</p>
            )}
            <div className="attempts">
              <Attempt title="Baseline" result={data.baseline} />
              <Attempt title="Candidate" result={data.candidate} />
            </div>
          </>
        )}
      </Show>
      <p><Link to={`/runs/${encodeURIComponent(id)}/endpoints`}>All endpoints</Link></p>
    </>
  );
}
