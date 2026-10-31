import json

import verify_run

LOG = 'INFO:     172.18.0.1:1 - "GET /api/a?x=1 HTTP/1.1" 200 OK\n'


def write_run(tmp_path, results=None, run=None, log=LOG):
    trace = [
        {
            "request_id": "r1",
            "method": "GET",
            "path": "/api/a?x=1",
            "arrival_offset_ms": 10,
            "replayable": True,
        },
        {
            "request_id": "r2",
            "method": "POST",
            "path": "/api/b",
            "arrival_offset_ms": 20,
            "replayable": False,
        },
    ]
    good = [
        {
            "request_id": "r1",
            "target": target,
            "status": 200,
            "error_category": "none",
            "scheduled_offset_ms": 10,
            "actual_start_offset_ms": 10.5,
            "scheduling_delay_ms": 0.5,
        }
        for target in verify_run.TARGETS
    ]
    run_doc = {
        "status": "completed",
        "baseline_seed": "x",
        "candidate_seed": "x",
        "config": {"speed": 1},
    }
    (tmp_path / "t.jsonl").write_text("".join(json.dumps(r) + "\n" for r in trace))
    (tmp_path / "results.jsonl").write_text(
        "".join(json.dumps(r) + "\n" for r in (good if results is None else results(good)))
    )
    (tmp_path / "run.json").write_text(json.dumps({**run_doc, **(run or {})}))
    logs = {}
    for target in verify_run.TARGETS:
        path = tmp_path / f"{target}.log"
        path.write_text(log)
        logs[target] = str(path)
    return verify_run.check(str(tmp_path / "t.jsonl"), str(tmp_path), logs)


def test_good_run_passes(tmp_path):
    assert write_run(tmp_path) == []


def test_unreplayable_request_in_results_fails(tmp_path):
    def add(good):
        return good + [{**good[0], "request_id": "r2"}]

    assert any("cover" in p for p in write_run(tmp_path, results=add))


def test_request_missing_from_access_log_fails(tmp_path):
    assert any("not in its access log" in p for p in write_run(tmp_path, log=""))


def test_unexpected_request_in_access_log_fails(tmp_path):
    extra = LOG + 'INFO:  1 - "POST /api/x HTTP/1.1" 201 Created\n'
    assert any("not in results" in p for p in write_run(tmp_path, log=extra))


def test_different_seed_fails(tmp_path):
    assert any("seed" in p for p in write_run(tmp_path, run={"candidate_seed": "y"}))


def test_early_start_fails(tmp_path):
    def early(good):
        return [{**r, "actual_start_offset_ms": 5} for r in good]

    assert any("early" in p for p in write_run(tmp_path, results=early))


def delayed(target):
    def slow(good):
        return [{**r, "scheduling_delay_ms": 500} if r["target"] == target else r for r in good]

    return slow


def test_baseline_delay_fails_pacing(tmp_path):
    assert any("scheduling delay" in p for p in write_run(tmp_path, results=delayed("baseline")))


def test_candidate_queueing_is_a_result(tmp_path):
    assert write_run(tmp_path, results=delayed("candidate")) == []
