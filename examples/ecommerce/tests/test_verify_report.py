import verify_report

SEARCH = "GET /api/products"


def report(verdicts, comparisons=(), status="completed"):
    return {
        "provenance": {"status": status},
        "endpoints": [{"key": k, "verdict": v} for k, v in verdicts.items()],
        "comparisons": list(comparisons),
        "trial_differences": [],
    }


def test_clean_passes():
    assert verify_report.check(report({SEARCH: "no_regression"}), "clean", None) == []


def test_false_alarm_fails_even_when_only_observed():
    problems = verify_report.check(report({SEARCH: "observed_regression"}), "clean", None)
    assert any("false alarm" in p for p in problems)


def test_nothing_measured_fails():
    problems = verify_report.check(report({SEARCH: "insufficient_samples"}), "clean", None)
    assert any("enough samples" in p for p in problems)


def test_regression_found_passes():
    verdicts = {SEARCH: "confirmed_regression", "GET /a": "no_regression"}
    assert verify_report.check(report(verdicts), "regression", SEARCH) == []


def test_regression_only_observed_is_missed():
    problems = verify_report.check(report({SEARCH: "observed_regression"}), "regression", SEARCH)
    assert any("missed" in p for p in problems)


def test_wrong_endpoint_fails():
    verdicts = {SEARCH: "confirmed_regression", "GET /a": "confirmed_regression"}
    problems = verify_report.check(report(verdicts), "regression", SEARCH)
    assert any("wrong endpoint" in p for p in problems)


def trials(*different):
    return [{"trial": i, "compared": 1000, "different": d} for i, d in enumerate(different, 1)]


def with_differences(*different):
    out = report({SEARCH: "no_regression"}, [DIFFERENT])
    out["trial_differences"] = trials(*different)
    return out


DIFFERENT = {"endpoint": SEARCH, "classification": "body_mismatch", "count": 1}


def test_rare_overtaking_is_tolerated():
    assert verify_report.check(with_differences(1, 0, 5, 0, 2), "clean", None) == []


def test_one_disturbed_trial_is_tolerated():
    assert verify_report.check(with_differences(0, 44, 1, 0, 2), "clean", None) == []


def test_change_in_most_trials_fails():
    problems = verify_report.check(with_differences(0, 44, 30, 40, 2), "clean", None)
    assert any("body_mismatch" in p for p in problems)
