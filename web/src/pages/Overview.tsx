import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useApi } from "../api/useApi";
import { Empty, Show, VerdictBadge } from "../components/ui";
import { when, words } from "../format";

export function Overview() {
  const runs = useApi((signal) => api.runs(signal), []);
  return (
    <>
      <h1>Runs</h1>
      <Show value={runs} what="runs">
        {(list) =>
          list.length === 0 ? (
            <Empty>
              <strong>No runs yet.</strong> Create one with <code>make regression-demo</code>, then reload this page.
            </Empty>
          ) : (
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
                  {list.map((run) => (
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
          )
        }
      </Show>
    </>
  );
}
