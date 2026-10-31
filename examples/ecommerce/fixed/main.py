"""The candidate with its regression repaired, for before/after demos."""

from shop.app import create_app
from shop.db import seed_digest

app = create_app("fixed", probe=seed_digest)
