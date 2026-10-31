"""The backend under test, with a deliberate performance regression."""

from shop.app import create_app
from shop.db import seed_digest
from shop.store import PostgresStore


class NPlusOneStore(PostgresStore):
    """Search as it might look after a well-meant refactor to reuse get_product.

    INTENTIONAL DEFECT for the demo: one query to find matching IDs, then one more
    per row, each with its own pool checkout. Responses are identical to the
    baseline's; only the time to produce them grows with the page size.
    """

    def search_products(self, query: str, limit: int) -> list[dict]:
        with self.pool.connection() as conn:
            rows = conn.execute(
                "SELECT id FROM products WHERE name ILIKE %s ORDER BY id LIMIT %s",
                (f"%{query}%", limit),
            ).fetchall()
        return [self.get_product(row["id"]) for row in rows]


app = create_app("candidate", probe=seed_digest, store=NPlusOneStore())
