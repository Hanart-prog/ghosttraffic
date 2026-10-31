"""The known-good backend that replays are compared against."""

from shop.app import create_app
from shop.db import seed_digest

app = create_app("baseline", probe=seed_digest)
