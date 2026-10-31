"""The candidate with its regression repaired, for before/after demos."""

from shop.app import create_app
from shop.db import seed_digest
from shop.store import PRODUCT_COLUMNS, PostgresStore


class BatchedStore(PostgresStore):
    """Keeps the candidate's two-step shape but loads the rows in one query."""

    def search_products(self, query: str, limit: int) -> list[dict]:
        with self.pool.connection() as conn:
            rows = conn.execute(
                "SELECT id FROM products WHERE name ILIKE %s ORDER BY id LIMIT %s",
                (f"%{query}%", limit),
            ).fetchall()
            return conn.execute(
                f"SELECT {PRODUCT_COLUMNS} FROM products WHERE id = ANY(%s) ORDER BY id",
                ([row["id"] for row in rows],),
            ).fetchall()


app = create_app("fixed", probe=seed_digest, store=BatchedStore())
