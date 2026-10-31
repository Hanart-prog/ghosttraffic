import json

import pytest

import make_workflows


def record(request_id, method, path, offset=0):
    return {
        "request_id": request_id,
        "method": method,
        "path": path,
        "session_id": "s",
        "arrival_offset_ms": offset,
        "replayable": True,
    }


CHECKOUT = [
    record("r1", "GET", "/api/products?query=x"),
    record("r2", "GET", "/api/products/5"),
    record("r3", "POST", "/api/carts"),
    record("r4", "POST", "/api/carts/17/items"),
    record("r5", "GET", "/api/carts/17"),
    record("r6", "POST", "/api/orders"),
]


def test_checkout_is_chained_with_explicit_substitutions():
    steps = make_workflows.checkout("s", CHECKOUT)["steps"]
    assert steps[0].get("depends_on") is None
    assert steps[3]["depends_on"] == ["create-cart"]
    assert steps[2]["extract"] == {"cart_id": "$.cart.id"}
    assert steps[3]["substitute"]["path"] == {"/api/carts/17": "/api/carts/${cart_id}"}
    assert steps[5]["substitute"]["body"] == {"$.cart_id": "${cart_id}"}


@pytest.mark.parametrize("records", [CHECKOUT[:3], CHECKOUT[::-1]])
def test_other_shapes_are_left_alone(records):
    assert make_workflows.checkout("s", records) is None


def test_unknown_route_is_left_alone():
    assert make_workflows.checkout("s", [record("r", "DELETE", "/x")]) is None


def test_main_skips_unreplayable_records(tmp_path):
    trace = tmp_path / "t.jsonl"
    rows = CHECKOUT + [{**record("x", "POST", "/api/carts", 9), "replayable": False}]
    trace.write_text("".join(json.dumps(r) + "\n" for r in rows))
    out = tmp_path / "w.json"
    assert make_workflows.main([str(trace), str(out)]) == 0
    assert len(json.loads(out.read_text())["workflows"]) == 1
