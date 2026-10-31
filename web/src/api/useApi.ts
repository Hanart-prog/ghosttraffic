import { useEffect, useState } from "react";

export type Loadable<T> =
  | { state: "loading" }
  | { state: "error"; message: string; status?: number }
  | { state: "ready"; data: T };

// useApi loads data for the given dependencies. A newer request aborts the older
// one, so a slow response can never overwrite a newer page.
export function useApi<T>(load: (signal: AbortSignal) => Promise<T>, deps: unknown[]): Loadable<T> {
  const [value, setValue] = useState<Loadable<T>>({ state: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    setValue({ state: "loading" });
    load(controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) setValue({ state: "ready", data });
      },
      (error: Error & { status?: number }) => {
        if (!controller.signal.aborted) {
          setValue({ state: "error", message: error.message, status: error.status });
        }
      },
    );
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return value;
}
