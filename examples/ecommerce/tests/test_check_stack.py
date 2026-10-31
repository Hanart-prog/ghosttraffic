import pytest

import check_stack


@pytest.fixture
def healths(monkeypatch):
    responses = {}

    def fetch(url):
        result = responses[url]
        if isinstance(result, Exception):
            raise result
        return {"variant": url, "seed_digest": result}

    monkeypatch.setattr(check_stack, "fetch_health", fetch)
    return responses


def test_passes_when_digests_match(healths):
    healths.update(a="same", b="same")
    assert check_stack.main(["a", "b"]) == 0


def test_fails_when_digests_differ(healths):
    healths.update(a="one", b="two")
    assert check_stack.main(["a", "b"]) == 1


def test_fails_when_a_backend_is_down(healths):
    healths.update(a="same", b=OSError("refused"))
    assert check_stack.main(["a", "b"]) == 1


def test_requires_two_urls():
    assert check_stack.main(["a"]) == 2
