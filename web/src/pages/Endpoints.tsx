import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Endpoint } from "../api/types";
import { useApi } from "../api/useApi";
import { Empty, RunNav, Show, VerdictBadge } from "../components/ui";
import { ms } from "../format";

type Column = { key: string; label: string; numeric?: boolean; value: (e: Endpoint) => number | string | null };

const severity = { no_regression: 0, insufficient_samples: 1, observed_regression: 2, confirmed_regression: 3 };

const columns: Column[] = [
  { key: "key", label: "Endpoint", value: (e) => e.key },
  { key: "verdict", label: "Verdict", value: (e) => severity[e.verdict] },
  { key: "base95", label: "Baseline p95", numeric: true, value: (e) => e.baseline.response_time.p95_ms },
  { key: "cand95", label: "Candidate p95", numeric: true, value: (e) => e.candidate.response_time.p95_ms },
  { key: "base50", label: "Baseline p50", numeric: true, value: (e) => e.baseline.response_time.p50_ms },
  { key: "cand50", label: "Candidate p50", numeric: true, value: (e) => e.candidate.response_time.p50_ms },
  { key: "samples", label: "Samples", numeric: true, value: (e) => e.candidate.response_time.samples },
  { key: "errors", label: "Candidate errors", numeric: true, value: (e) => e.candidate.errors },
];

function Table({ endpoints }: { endpoints: Endpoint[] }) {
  const [sort, setSort] = useState({ key: "verdict", descending: true });
  const sorted = useMemo(() => {
    const column = columns.find((c) => c.key === sort.key) ?? columns[0]!;
    return [...endpoints].sort((a, b) => {
      const [x, y] = [column.value(a), column.value(b)];
      // Missing values sort last in either direction: they are not small numbers.
      if (x === null || y === null) return x === y ? 0 : x === null ? 1 : -1;
      const order = x < y ? -1 : x > y ? 1 : 0;
      return sort.descending ? -order : order;
    });
  }, [endpoints, sort]);
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            {columns.map((column) => {
              const active = sort.key === column.key;
              return (
                <th
                  key={column.key}
                  scope="col"
                  className={column.numeric ? "num" : undefined}
                  aria-sort={active ? (sort.descending ? "descending" : "ascending") : "none"}
                >
                  <button
                    className="sort"
                    onClick={() => setSort({ key: column.key, descending: active ? !sort.descending : true })}
                  >
                    {column.label} {active && <span aria-hidden="true">{sort.descending ? "↓" : "↑"}</span>}
                  </button>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {sorted.map((endpoint) => (
            <tr key={endpoint.key}>
              <td>
                <Link to={encodeURIComponent(endpoint.key)}>
                  <code>{endpoint.key}</code>
                </Link>
              </td>
              <td>
                <VerdictBadge verdict={endpoint.verdict} />
              </td>
              <td className="num">{ms(endpoint.baseline.response_time.p95_ms)}</td>
              <td className="num">{ms(endpoint.candidate.response_time.p95_ms)}</td>
              <td className="num">{ms(endpoint.baseline.response_time.p50_ms)}</td>
              <td className="num">{ms(endpoint.candidate.response_time.p50_ms)}</td>
              <td className="num">{endpoint.candidate.response_time.samples}</td>
              <td className="num">{endpoint.candidate.errors}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function Endpoints() {
  const { id = "" } = useParams();
  const endpoints = useApi((signal) => api.endpoints(id, signal), [id]);
  return (
    <>
      <div className="page-head">
        <span className="tag">{id}</span>
        <h1>Endpoints</h1>
      </div>
      <RunNav />
      <Show value={endpoints} what="endpoints">
        {(list) => (list.length === 0 ? <Empty>This run recorded no endpoints.</Empty> : <Table endpoints={list} />)}
      </Show>
    </>
  );
}
