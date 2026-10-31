import json

import verify_trace


def sent(**overrides):
    entry = {
        "session": "session_01",
        "method": "POST",
        "path": "/api/carts?access_token=FAKE-SECRET-a",
        "body": {"customer_id": 1, "password": "FAKE-SECRET-pw"},
        "raw_bytes": None,
        "status": 201,
    }
    return {**entry, **overrides}


def record(**overrides):
    entry = {
        "request_id": "req_000001",
        "session_id": verify_trace.session_id("session_01"),
        "arrival_offset_ms": 0,
        "method": "POST",
        "path": "/api/carts?access_token=[REDACTED]",
        "headers": {"content-type": "application/json"},
        "body_json": {"customer_id": 1, "password": "[REDACTED]"},
        "capture": {"status": 201, "duration_ms": 1.5},
        "replayable": True,
    }
    return {**entry, **overrides}


def run(tmp_path, records, sent_entries):
    trace, log = tmp_path / "trace.jsonl", tmp_path / "sent.jsonl"
    trace.write_text("".join(json.dumps(r) + "\n" for r in records))
    log.write_text("".join(json.dumps(s) + "\n" for s in sent_entries))
    return verify_trace.check(str(trace), str(log))


def test_matching_trace_passes(tmp_path):
    assert run(tmp_path, [record()], [sent()]) == []


def test_leaked_secret_fails(tmp_path):
    leaked = record(body_json={"customer_id": 1, "password": "FAKE-SECRET-pw"})
    assert any("forbidden" in p for p in run(tmp_path, [leaked], [sent()]))


def test_missing_request_fails(tmp_path):
    assert any("recorded" in p for p in run(tmp_path, [], [sent()]))


def test_altered_status_fails(tmp_path):
    altered = record(capture={"status": 500, "duration_ms": 1})
    assert any("missing" in p for p in run(tmp_path, [altered], [sent()]))


def test_extra_header_fails(tmp_path):
    assert any("headers" in p for p in run(tmp_path, [record(headers={"x-a": "b"})], [sent()]))


def test_wrong_order_fails(tmp_path):
    first, second = sent(path="/a"), sent(path="/b")
    records = [
        record(request_id="r1", path="/b", arrival_offset_ms=0),
        record(request_id="r2", path="/a", arrival_offset_ms=5),
    ]
    assert any("order" in p for p in run(tmp_path, records, [first, second]))
