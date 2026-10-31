import type { Endpoint, RequestDetail, RequestPage, RunDetail, RunSummary, Timeline } from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal });
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as { error?: string } | null;
    throw new ApiError(body?.error ?? `Request failed (${response.status})`, response.status);
  }
  return (await response.json()) as T;
}

const run = (id: string) => `/api/runs/${encodeURIComponent(id)}`;

function query(params: Record<string, string | number | null | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== null && value !== undefined && value !== "") search.set(key, String(value));
  }
  const text = search.toString();
  return text ? `?${text}` : "";
}

export const api = {
  runs: (signal?: AbortSignal) => get<RunSummary[]>("/api/runs", signal),
  run: (id: string, signal?: AbortSignal) => get<RunDetail>(run(id), signal),
  endpoints: (id: string, signal?: AbortSignal) => get<Endpoint[]>(`${run(id)}/endpoints`, signal),
  requests: (
    id: string,
    params: { endpoint?: string; target?: string; trial?: string; cursor?: number | null; limit?: number },
    signal?: AbortSignal,
  ) => get<RequestPage>(`${run(id)}/requests${query(params)}`, signal),
  request: (id: string, requestId: string, trial: string, signal?: AbortSignal) =>
    get<RequestDetail>(`${run(id)}/requests/${encodeURIComponent(requestId)}${query({ trial })}`, signal),
  timeline: (
    id: string,
    params: { trial: string; target: string; endpoint?: string; offset: number; limit: number },
    signal?: AbortSignal,
  ) => get<Timeline>(`${run(id)}/timeline${query(params)}`, signal),
};
