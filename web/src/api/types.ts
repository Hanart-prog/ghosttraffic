// Mirrors the Go API in internal/server. Null means "not available", never zero.

export type Verdict =
  | "insufficient_samples"
  | "no_regression"
  | "observed_regression"
  | "confirmed_regression";

export interface RequestCounts {
  total: number;
  sent: number;
  skipped: number;
}

export interface RunSummary {
  run_id: string;
  status: string;
  started_at: string;
  ended_at: string | null;
  baseline: string;
  candidate: string;
  requests: RequestCounts;
  verdict: Verdict | null;
}

export interface Latency {
  samples: number;
  p50_ms: number | null;
  p95_ms: number | null;
  p99_ms: number | null;
}

export interface Bucket {
  upper_ms: number;
  count: number;
}

export interface TargetStats {
  total: number;
  successes: number;
  errors: number;
  transport_errors: number;
  dropped: number;
  throughput_rps: number;
  latency: Latency;
  response_time: Latency;
  scheduling_delay: Latency;
  histogram: Bucket[];
}

export interface TrialComparison {
  trial: number;
  baseline_p95_ms: number | null;
  candidate_p95_ms: number | null;
  baseline_samples: number;
  candidate_samples: number;
}

export interface Endpoint {
  key: string;
  verdict: Verdict;
  baseline: TargetStats;
  candidate: TargetStats;
  trials: TrialComparison[];
}

export interface ComparisonStat {
  endpoint: string;
  classification: string;
  count: number;
}

export interface Difference {
  endpoint: string;
  request_id: string;
  trial: number;
  classification: string;
  baseline_status: number;
  candidate_status: number;
  paths: string[];
}

export interface Report {
  run_id: string;
  verdict: Verdict;
  thresholds: { min_samples: number; min_ratio: number; min_delta_ms: number };
  ignored_json_paths: string[];
  summary: { baseline: TargetStats; candidate: TargetStats };
  endpoints: Endpoint[];
  comparisons: ComparisonStat[];
  differences: Difference[];
  warnings: string[];
  provenance: { trials: number; trace_sha256: string };
}

export interface Run {
  run_id: string;
  status: string;
  baseline: string;
  candidate: string;
  started_at: string;
  warnings: string[];
  requests: RequestCounts;
}

export interface RunDetail {
  run: Run;
  report: Report | null;
  report_error?: string;
}

export interface Result {
  trial: number;
  request_id: string;
  target: "baseline" | "candidate";
  endpoint_key: string;
  scheduled_offset_ms: number;
  actual_start_offset_ms: number;
  scheduling_delay_ms: number;
  dependency_wait_ms?: number;
  duration_ms: number;
  status: number;
  error_category: string;
  response_json?: unknown;
}

export interface RequestPage {
  items: Result[];
  next_cursor: number | null;
}

export interface RecordedRequest {
  method: string;
  path: string;
  headers: Record<string, string> | null;
  body_json: unknown;
}

export interface RequestDetail {
  request_id: string;
  trial: number;
  request: RecordedRequest | null;
  baseline: Result | null;
  candidate: Result | null;
  classification: string;
  differing_paths: string[];
  ignored_paths: string[];
}

export interface TimelineItem {
  request_id: string;
  endpoint_key: string;
  status: number;
  error_category: string;
  scheduled_offset_ms: number;
  dependency_wait_ms: number;
  scheduling_delay_ms: number;
  actual_start_offset_ms: number;
  duration_ms: number;
}

export interface Timeline {
  trial: number;
  target: string;
  total: number;
  offset: number;
  items: TimelineItem[];
}
