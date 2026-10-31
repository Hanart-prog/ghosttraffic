import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { App } from "./App";
import type { Endpoint, Latency, TargetStats } from "./api/types";
import { change } from "./format";

const latency = (p50: number | null, p95: number | null): Latency => ({
  samples: p95 === null ? 0 : 750,
  p50_ms: p50,
  p95_ms: p95,
  p99_ms: p95,
});

const stats = (p95: number | null): TargetStats => ({
  total: 750,
  successes: 750,
  errors: 2,
  transport_errors: 0,
  dropped: 0,
  throughput_rps: 90,
  latency: latency(2, p95),
  response_time: latency(2, p95),
  scheduling_delay: latency(0, 0),
  histogram: [{ upper_ms: 5, count: 700 }],
});

const endpoint = (key: string, verdict: Endpoint["verdict"], a: number, b: number | null): Endpoint => ({
  key,
  verdict,
  baseline: stats(a),
  candidate: stats(b),
  trials: [{ trial: 1, baseline_p95_ms: a, candidate_p95_ms: b, baseline_samples: 150, candidate_samples: 150 }],
});

const endpoints = [
  endpoint("GET /quiet", "no_regression", 3, 3),
  endpoint("GET /slow", "confirmed_regression", 4, 16),
  endpoint("GET /none", "insufficient_samples", 9, null),
];

const report = {
  run_id: "r1",
  verdict: "confirmed_regression",
  thresholds: { min_samples: 100, min_ratio: 1.2, min_delta_ms: 5 },
  ignored_json_paths: ["$.id"],
  summary: { baseline: stats(4), candidate: stats(16) },
  endpoints,
  comparisons: [],
  differences: [{ endpoint: "GET /slow", request_id: "req 1", trial: 2, classification: "body_mismatch", baseline_status: 200, candidate_status: 200, paths: ["$.v"] }],
  warnings: ["Something to note"],
  provenance: { trials: 2, trace_sha256: "ab" },
};

const run = {
  run_id: "r1",
  status: "completed",
  baseline: "http://b",
  candidate: "http://c",
  started_at: "2026-01-02T03:04:05Z",
  warnings: [],
  requests: { total: 3, sent: 3, skipped: 1 },
};

const summary = { ...run, ended_at: null, verdict: "confirmed_regression" };

const result = (target: string, overrides = {}) => ({
  trial: 1,
  request_id: "r0",
  target,
  endpoint_key: "GET /slow",
  scheduled_offset_ms: 10,
  actual_start_offset_ms: 12,
  scheduling_delay_ms: 2,
  duration_ms: 5,
  status: 200,
  error_category: "none",
  ...overrides,
});

type Routes = Record<string, unknown>;

function mockApi(routes: Routes) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      calls.push(input);
      const path = input.split("?")[0]!;
      if (!(path in routes)) {
        return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      }
      const body = routes[path];
      return new Response(JSON.stringify(typeof body === "function" ? body(input) : body), { status: 200 });
    }),
  );
  return calls;
}

function open(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe("states", () => {
  it("shows loading, then the empty state, with no invented rows", async () => {
    mockApi({ "/api/runs": [] });
    open("/");
    expect(screen.getByRole("status")).toHaveTextContent("Loading runs");
    expect(await screen.findByText("No runs yet.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("shows the API's own error message", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "disk on fire" }), { status: 500 })));
    open("/");
    expect(await screen.findByRole("alert")).toHaveTextContent("disk on fire");
  });

  it("survives a network failure", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("Failed to fetch"); }));
    open("/");
    expect(await screen.findByRole("alert")).toHaveTextContent("Failed to fetch");
  });

  it("has a page for unknown addresses", async () => {
    mockApi({});
    open("/nope");
    expect(await screen.findByText(/doesn't exist/)).toBeInTheDocument();
  });

  it("explains a run with no report", async () => {
    mockApi({ "/api/runs/r1": { run, report: null } });
    open("/runs/r1");
    expect(await screen.findByText("This run has no report yet.")).toBeInTheDocument();
  });

  it("says when a report cannot be read", async () => {
    mockApi({ "/api/runs/r1": { run, report: null, report_error: "cannot be read" } });
    open("/runs/r1");
    expect(await screen.findByText("cannot be read")).toBeInTheDocument();
  });
});

describe("overview", () => {
  it("lists runs and links to them", async () => {
    mockApi({ "/api/runs": [summary], "/api/runs/r1": { run, report } });
    open("/");
    await userEvent.click(await screen.findByRole("link", { name: "r1" }));
    expect(await screen.findByText("Warnings")).toBeInTheDocument();
  });

  it("labels a run without a verdict rather than implying one", async () => {
    mockApi({ "/api/runs": [{ ...summary, verdict: null }] });
    open("/");
    expect(await screen.findByText("No report")).toBeInTheDocument();
  });
});

describe("theme switch", () => {
  it("remembers the choice and applies it", async () => {
    localStorage.clear();
    mockApi({ "/api/runs": [] });
    open("/");
    await userEvent.click(screen.getByRole("button", { name: "Switch to dark theme" }));
    expect(document.documentElement).toHaveAttribute("data-theme", "dark");
    expect(localStorage.getItem("ghosttraffic-theme")).toBe("dark");
    await userEvent.click(screen.getByRole("button", { name: "Switch to light theme" }));
    expect(document.documentElement).toHaveAttribute("data-theme", "light");
  });
});

describe("trial spread", () => {
  it("draws one dot per trial and says how many were slower", async () => {
    mockApi({ "/api/runs/r1/endpoints": endpoints });
    open("/runs/r1/endpoints/GET%20%2Fslow");
    expect(await screen.findByRole("img", { name: "Candidate trial 1: 16.0 ms" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Baseline trial 1: 4.00 ms" })).toBeInTheDocument();
    expect(screen.getByText("1 of 1")).toHaveClass("stat-value");
  });

  it("leaves out a trial without a p95", async () => {
    mockApi({ "/api/runs/r1/endpoints": endpoints });
    open("/runs/r1/endpoints/GET%20%2Fnone");
    await screen.findByRole("img", { name: "Baseline trial 1: 9.00 ms" });
    expect(screen.queryByRole("img", { name: /^Candidate trial/ })).not.toBeInTheDocument();
    expect(screen.getByText("0 of 1")).toHaveClass("stat-value");
  });
});

describe("run detail", () => {
  it("shows values from the report", async () => {
    mockApi({ "/api/runs/r1": { run, report } });
    open("/runs/r1");
    expect((await screen.findAllByText("16.0 ms"))[0]).toHaveClass("stat-value");
    expect(screen.getByText("Something to note")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "req 1" })).toHaveAttribute("href", "/runs/r1/requests/req%201?trial=2");
  });

  it("names the worst endpoint and the change from baseline", async () => {
    mockApi({ "/api/runs/r1": { run, report } });
    open("/runs/r1");
    expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("Confirmed regression");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(/GET \/slow, \d+\.\d× baseline p95/);
    expect(screen.getAllByText("+300%").length).toBeGreaterThan(0);
  });

  it("leaves out a change that cannot be computed", () => {
    expect(change(5, 0)).toBeUndefined();
    expect(change(null, 5)).toBeUndefined();
    expect(change(9, 10)).toBe("−10%");
  });

  it("does not list equal failures as differences", async () => {
    const differences = [{ ...report.differences[0]!, classification: "both_failed" }];
    mockApi({ "/api/runs/r1": { run, report: { ...report, differences } } });
    open("/runs/r1");
    await screen.findByText("Something to note");
    expect(screen.queryByText("Response differences")).not.toBeInTheDocument();
  });

  it("does not invent an error rate with no requests", async () => {
    const empty = { ...stats(null), total: 0 };
    mockApi({ "/api/runs/r1": { run, report: { ...report, summary: { baseline: empty, candidate: empty } } } });
    open("/runs/r1");
    expect((await screen.findAllByText("n/a")).length).toBeGreaterThan(0);
  });
});

describe("endpoints", () => {
  const rows = () => screen.getAllByRole("row").slice(1).map((row) => within(row).getAllByRole("cell")[0]!.textContent);

  it("starts with the worst verdict, and sorts by clicking", async () => {
    mockApi({ "/api/runs/r1/endpoints": endpoints });
    open("/runs/r1/endpoints");
    await screen.findByText("GET /slow");
    expect(rows()[0]).toBe("GET /slow");
    await userEvent.click(screen.getByRole("button", { name: /^Endpoint/ }));
    expect(rows()).toEqual(["GET /slow", "GET /quiet", "GET /none"]);
    await userEvent.click(screen.getByRole("button", { name: /^Endpoint/ }));
    expect(rows()).toEqual(["GET /none", "GET /quiet", "GET /slow"]);
  });

  it("keeps missing values last in both directions", async () => {
    mockApi({ "/api/runs/r1/endpoints": endpoints });
    open("/runs/r1/endpoints");
    await screen.findByText("GET /slow");
    const header = screen.getByRole("button", { name: /^Candidate p95/ });
    await userEvent.click(header);
    expect(rows().at(-1)).toBe("GET /none");
    await userEvent.click(header);
    expect(rows().at(-1)).toBe("GET /none");
  });

  it("marks the sorted column for assistive technology", async () => {
    mockApi({ "/api/runs/r1/endpoints": endpoints });
    open("/runs/r1/endpoints");
    await screen.findByText("GET /slow");
    await userEvent.click(screen.getByRole("button", { name: /^Samples/ }));
    expect(screen.getByRole("columnheader", { name: /Samples/ })).toHaveAttribute("aria-sort", "descending");
  });
});

describe("endpoint detail", () => {
  const key = encodeURIComponent("GET /slow");
  const routes = (requests: unknown): Routes => ({
    "/api/runs/r1/endpoints": endpoints,
    "/api/runs/r1/requests": requests,
  });

  it("filters and pages through real API calls", async () => {
    const calls = mockApi(routes(() => ({ items: [result("baseline")], next_cursor: 99 })));
    open(`/runs/r1/endpoints/${key}`);
    await screen.findByRole("link", { name: "r0" });
    await userEvent.selectOptions(screen.getByLabelText("Target"), "candidate");
    await screen.findByRole("link", { name: "r0" });
    expect(calls.at(-1)).toContain("target=candidate");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByRole("link", { name: "r0" });
    expect(calls.at(-1)).toContain("cursor=99");
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    await screen.findByRole("link", { name: "r0" });
    expect(calls.at(-1)).not.toContain("cursor");
  });

  it("returns to the first page when a filter changes", async () => {
    const calls = mockApi(routes(() => ({ items: [result("baseline")], next_cursor: 99 })));
    open(`/runs/r1/endpoints/${key}`);
    await screen.findByRole("link", { name: "r0" });
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByRole("link", { name: "r0" });
    await userEvent.selectOptions(screen.getByLabelText("Trial"), "1");
    await screen.findByRole("link", { name: "r0" });
    expect(calls.at(-1)).not.toContain("cursor");
  });

  it("disables paging at the ends", async () => {
    mockApi(routes({ items: [result("baseline")], next_cursor: null }));
    open(`/runs/r1/endpoints/${key}`);
    await screen.findByRole("link", { name: "r0" });
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  });

  it("says when no requests match", async () => {
    mockApi(routes({ items: [], next_cursor: null }));
    open(`/runs/r1/endpoints/${key}`);
    expect(await screen.findByText("No requests match these filters.")).toBeInTheDocument();
  });

  it("handles an endpoint the run does not have", async () => {
    mockApi(routes({ items: [], next_cursor: null }));
    open(`/runs/r1/endpoints/${encodeURIComponent("GET /gone")}`);
    expect(await screen.findByText("This run has no endpoint with that name.")).toBeInTheDocument();
  });
});

describe("timeline", () => {
  const item = { request_id: "r0", endpoint_key: "GET /slow", status: 200, error_category: "none", scheduled_offset_ms: 0, dependency_wait_ms: 25, scheduling_delay_ms: 25, actual_start_offset_ms: 50, duration_ms: 50 };
  const data = (overrides = {}) => ({ trial: 1, target: "baseline", total: 1, offset: 0, items: [item], ...overrides });

  it("draws queueing and dependency wait in position", async () => {
    mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data() });
    const { container } = render(
      <MemoryRouter initialEntries={["/runs/r1/timeline"]}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByRole("list", { name: /due-time order/ });
    const style = (name: string) => container.querySelector<HTMLElement>(`.seg.${name}`)!.style;
    expect(style("wait").left).toBe("0%");
    expect(style("wait").width).toBe("25%");
    expect(style("delay").left).toBe("25%");
    expect(style("run").left).toBe("50%");
    expect(style("run").width).toBe("50%");
  });

  it("scales the axis to the window, so late requests stay readable", async () => {
    const late = { ...item, scheduled_offset_ms: 1000, dependency_wait_ms: 0, scheduling_delay_ms: 0, actual_start_offset_ms: 1000, duration_ms: 10 };
    mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data({ items: [late] }) });
    const { container } = render(
      <MemoryRouter initialEntries={["/runs/r1/timeline"]}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByRole("list", { name: /due-time order/ });
    const bar = container.querySelector<HTMLElement>(".seg.run")!.style;
    expect(bar.left).toBe("0%");
    expect(bar.width).toBe("100%");
  });

  it("gives exact times to people not using a mouse", async () => {
    mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data() });
    open("/runs/r1/timeline");
    expect(await screen.findByRole("link", { name: /queued, ran 50\.0 ms/ })).toBeInTheDocument();
  });

  it("switches target through the API", async () => {
    const calls = mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data() });
    open("/runs/r1/timeline");
    await screen.findByRole("list", { name: /due-time order/ });
    await userEvent.selectOptions(screen.getByLabelText("Target"), "candidate");
    await screen.findByRole("list", { name: /due-time order/ });
    expect(calls.at(-1)).toContain("target=candidate");
  });

  it("offers every trial in the report", async () => {
    mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data() });
    open("/runs/r1/timeline");
    await screen.findByRole("list", { name: /due-time order/ });
    expect(within(screen.getByLabelText("Trial")).getAllByRole("option")).toHaveLength(2);
  });

  it("marks failures with more than color", async () => {
    mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data({ items: [{ ...item, status: 500 }] }) });
    open("/runs/r1/timeline");
    expect(await screen.findByLabelText("failed")).toBeInTheDocument();
  });

  it("has an empty state", async () => {
    mockApi({ "/api/runs/r1": { run, report }, "/api/runs/r1/timeline": data({ total: 0, items: [] }) });
    open("/runs/r1/timeline");
    expect(await screen.findByText("No requests for this target and trial.")).toBeInTheDocument();
  });
});

describe("request detail", () => {
  const detail = {
    request_id: "r0",
    trial: 1,
    request: { method: "POST", path: "/api/carts", headers: {}, body_json: { password: "[REDACTED]" } },
    baseline: result("baseline", { response_json: { v: 1 } }),
    candidate: result("candidate", { response_json: { v: 2 } }),
    classification: "body_mismatch",
    differing_paths: ["$.v"],
    ignored_paths: ["$.id"],
  };

  it("shows both attempts and what differed", async () => {
    mockApi({ "/api/runs/r1/requests/r0": detail });
    open("/runs/r1/requests/r0?trial=1");
    expect(await screen.findByText("$.v")).toBeInTheDocument();
    expect(screen.getByText("Body mismatch")).toBeInTheDocument();
    expect(screen.getAllByText("Completed")).toHaveLength(2);
    expect(screen.queryByText("None")).not.toBeInTheDocument();
    expect(screen.getByText(/POST \/api\/carts/)).toBeInTheDocument();
    expect(screen.getByText(/REDACTED/)).toBeInTheDocument();
  });

  it("says not available for bodies that were not captured, never a match", async () => {
    mockApi({
      "/api/runs/r1/requests/r0": { ...detail, baseline: result("baseline"), candidate: result("candidate"), classification: "body_unavailable", differing_paths: [] },
    });
    open("/runs/r1/requests/r0");
    expect((await screen.findAllByText(/Not available\. Bodies are stored/))).toHaveLength(2);
    expect(screen.getByText("Body unavailable")).toBeInTheDocument();
  });

  it("copes with one target missing and an old run", async () => {
    mockApi({ "/api/runs/r1/requests/r0": { ...detail, request: null, candidate: null, classification: "", differing_paths: [] } });
    open("/runs/r1/requests/r0");
    expect(await screen.findByText("No result for this target.")).toBeInTheDocument();
    expect(screen.getByText(/predates stored requests/)).toBeInTheDocument();
  });

  it("shows a missing request as an error", async () => {
    mockApi({});
    open("/runs/r1/requests/r0");
    expect(await screen.findByRole("alert")).toHaveTextContent("not found");
  });
});
