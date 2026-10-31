"""The backend under test. Its intentional regression arrives in Phase 3."""

from shop.app import create_app
from shop.db import seed_digest

app = create_app("candidate", probe=seed_digest)
