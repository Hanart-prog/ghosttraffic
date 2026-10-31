import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Timeline as Data, TimelineItem } from "../api/types";
import { useApi } from "../api/useApi";
import { Empty, RunNav, Show } from "../components/ui";
import { ms } from "../format";

// Small enough that a few milliseconds is visible on the shared axis.
const WINDOW = 40;

function Row({ id, item, start, span, trial }: { id: string; item: TimelineItem; start: number; span: number; trial: number }) {
  const left = (value: number) => `${((value - start) / span) * 100}%`;
  const width = (value: number) => `${(value / span) * 100}%`;
  const delayStart = item.scheduled_offset_ms + item.dependency_wait_ms;
  const failed = item.status >= 400 || item.error_category !== "none";
  const label =
    `${item.request_id} ${item.endpoint_key}: due ${ms(item.scheduled_offset_ms)}, waited ${ms(item.dependency_wait_ms)} ` +
    `for dependencies, ${ms(item.scheduling_delay_ms)} queued, ran ${ms(item.duration_ms)}, status ${item.status || "none"}`;
  return (
    <li className="row">
      <Link className="row-label" aria-label={label} to={`/runs/${encodeURIComponent(id)}/requests/${encodeURIComponent(item.request_id)}?trial=${trial}`}>
        {failed && <span aria-label="failed">✕ </span>}
        <code>{item.endpoint_key}</code>
      </Link>
      <div className="track" title={label}>
        <span className="seg wait" style={{ left: left(item.scheduled_offset_ms), width: width(item.dependency_wait_ms) }} />
        <span className="seg delay" style={{ left: left(delayStart), width: width(item.scheduling_delay_ms) }} />
        <span className="seg run" style={{ left: left(item.actual_start_offset_ms), width: width(item.duration_ms) }} />
      </div>
    </li>
  );
}

function Chart({ id, data }: { id: string; data: Data }) {
  const start = Math.min(...data.items.map((i) => i.scheduled_offset_ms));
  const end = Math.max(...data.items.map((i) => i.actual_start_offset_ms + i.duration_ms));
  // A window of instantly dropped requests would otherwise divide by zero.
  const span = Math.max(end - start, 1);
  return (
    <>
      <div className="legend">
        <span><span className="key wait" /> Waiting for earlier step</span>
        <span><span className="key delay" /> Queued</span>
        <span><span className="key run" /> Running</span>
      </div>
      <ol className="timeline" aria-label="Requests in due-time order">
        {data.items.map((item) => (
          <Row key={item.request_id} id={id} item={item} start={start} span={span} trial={data.trial} />
        ))}
      </ol>
      <p className="muted">
        Bars share one axis, from {ms(start)} to {ms(start + span)} after the pass began. Hover a bar for exact times.
      </p>
    </>
  );
}

export function Timeline() {
  const { id = "" } = useParams();
  const [target, setTarget] = useState("baseline");
  const [trial, setTrial] = useState("1");
  const [offset, setOffset] = useState(0);
  const run = useApi((signal) => api.run(id, signal), [id]);
  const trials = run.state === "ready" ? (run.data.report?.provenance.trials ?? 1) : 1;
  const timeline = useApi(
    (signal) => api.timeline(id, { trial, target, offset, limit: WINDOW }, signal),
    [id, trial, target, offset],
  );
  useEffect(() => setOffset(0), [target, trial]);

  return (
    <>
      <h1>{id}</h1>
      <RunNav />
      <div className="filters">
        <label>
          Target{" "}
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="baseline">Baseline</option>
            <option value="candidate">Candidate</option>
          </select>
        </label>
        <label>
          Trial{" "}
          <select value={trial} onChange={(e) => setTrial(e.target.value)}>
            {Array.from({ length: trials }, (_, i) => (
              <option key={i + 1} value={i + 1}>
                {i + 1}
              </option>
            ))}
          </select>
        </label>
      </div>
      <Show value={timeline} what="the timeline">
        {(data) =>
          data.total === 0 ? (
            <Empty>No requests for this target and trial.</Empty>
          ) : (
            <>
              <p className="muted">
                Showing {data.offset + 1} to {data.offset + data.items.length} of {data.total} requests.
              </p>
              <Chart id={id} data={data} />
              <div className="pager">
                <button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - WINDOW))}>
                  Previous
                </button>
                <button disabled={offset + WINDOW >= data.total} onClick={() => setOffset(offset + WINDOW)}>
                  Next
                </button>
              </div>
            </>
          )
        }
      </Show>
    </>
  );
}
