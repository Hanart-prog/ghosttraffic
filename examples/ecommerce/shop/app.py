"""Application factory shared by the baseline, candidate, and fixed backends."""

from collections.abc import Callable

from fastapi import FastAPI, Response


def create_app(variant: str, probe: Callable[[], str]) -> FastAPI:
    """Build a backend. `probe` returns the seed digest or raises if the database is down."""
    app = FastAPI(title=f"ghostshop-{variant}")

    @app.get("/health")
    def health(response: Response) -> dict[str, str | None]:
        try:
            digest = probe()
        except Exception:
            response.status_code = 503
            return {"status": "unavailable", "variant": variant, "seed_digest": None}
        return {"status": "ok", "variant": variant, "seed_digest": digest}

    return app
