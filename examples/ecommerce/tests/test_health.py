import pathlib

import pytest
from fastapi.testclient import TestClient

from shop.app import create_app


def test_health_reports_variant_and_digest():
    client = TestClient(create_app("candidate", probe=lambda: "abc", store=None))
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok", "variant": "candidate", "seed_digest": "abc"}


def test_health_is_unavailable_when_database_is_down():
    def probe() -> str:
        raise OSError("connection refused")

    client = TestClient(create_app("baseline", probe=probe, store=None))
    response = client.get("/health")
    assert response.status_code == 503
    assert response.json()["seed_digest"] is None


@pytest.mark.parametrize("path", ["001_schema.sql", "002_seed.sql"])
def test_seed_files_are_deterministic(path):
    root = pathlib.Path(__file__).parent.parent
    sql = (root / "seed" / path).read_text().lower()
    statements = "\n".join(line for line in sql.splitlines() if not line.startswith("--"))
    assert "random(" not in statements
    assert "now(" not in statements
