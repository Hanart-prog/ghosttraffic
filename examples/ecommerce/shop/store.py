"""Postgres queries behind the shop routes."""

import os

from psycopg import errors
from psycopg.rows import dict_row
from psycopg_pool import ConnectionPool

PRODUCT_COLUMNS = "id, sku, name, category, price_cents, stock"


class NotFound(Exception):
    pass


class Conflict(Exception):
    pass


class PostgresStore:
    def __init__(self) -> None:
        # A fixed, fully opened pool: a pool growing under load would measure its
        # own startup, and differently on each backend. /health is only reachable
        # after this returns.
        self.pool = ConnectionPool(
            os.environ["DATABASE_URL"], min_size=10, max_size=10, kwargs={"row_factory": dict_row}
        )
        self.pool.wait(timeout=30)

    def search_products(self, query: str, limit: int) -> list[dict]:
        with self.pool.connection() as conn:
            return conn.execute(
                f"SELECT {PRODUCT_COLUMNS} FROM products WHERE name ILIKE %s ORDER BY id LIMIT %s",
                (f"%{query}%", limit),
            ).fetchall()

    def get_product(self, product_id: int) -> dict:
        with self.pool.connection() as conn:
            row = conn.execute(
                f"SELECT {PRODUCT_COLUMNS} FROM products WHERE id = %s", (product_id,)
            ).fetchone()
        if row is None:
            raise NotFound(f"product {product_id}")
        return row

    def create_cart(self, customer_id: int) -> dict:
        with self.pool.connection() as conn:
            try:
                row = conn.execute(
                    "INSERT INTO carts (customer_id) VALUES (%s) RETURNING id", (customer_id,)
                ).fetchone()
            except errors.ForeignKeyViolation as exc:
                raise NotFound(f"customer {customer_id}") from exc
        return self.get_cart(row["id"])

    def add_item(self, cart_id: int, product_id: int, quantity: int) -> dict:
        with self.pool.connection() as conn:
            try:
                conn.execute(
                    """
                    INSERT INTO cart_items (cart_id, product_id, quantity) VALUES (%s, %s, %s)
                    ON CONFLICT (cart_id, product_id)
                    DO UPDATE SET quantity = cart_items.quantity + EXCLUDED.quantity
                    """,
                    (cart_id, product_id, quantity),
                )
            except errors.ForeignKeyViolation as exc:
                raise NotFound(f"cart {cart_id} or product {product_id}") from exc
        return self.get_cart(cart_id)

    def get_cart(self, cart_id: int) -> dict:
        with self.pool.connection() as conn:
            cart = conn.execute(
                "SELECT id, customer_id FROM carts WHERE id = %s", (cart_id,)
            ).fetchone()
            if cart is None:
                raise NotFound(f"cart {cart_id}")
            cart["items"] = conn.execute(
                """
                SELECT i.product_id, i.quantity, p.price_cents
                FROM cart_items i JOIN products p ON p.id = i.product_id
                WHERE i.cart_id = %s ORDER BY i.product_id
                """,
                (cart_id,),
            ).fetchall()
        return cart

    def create_order(self, cart_id: int) -> dict:
        with self.pool.connection() as conn:
            cart = conn.execute("SELECT id FROM carts WHERE id = %s", (cart_id,)).fetchone()
            if cart is None:
                raise NotFound(f"cart {cart_id}")
            total = conn.execute(
                """
                SELECT sum(i.quantity * p.price_cents) AS cents
                FROM cart_items i JOIN products p ON p.id = i.product_id
                WHERE i.cart_id = %s
                """,
                (cart_id,),
            ).fetchone()["cents"]
            if total is None:
                raise Conflict(f"cart {cart_id} is empty")
            try:
                conn.execute(
                    """
                    UPDATE products p SET stock = p.stock - i.quantity
                    FROM cart_items i WHERE i.product_id = p.id AND i.cart_id = %s
                    """,
                    (cart_id,),
                )
                return conn.execute(
                    """
                    INSERT INTO orders (cart_id, total_cents) VALUES (%s, %s)
                    RETURNING id, cart_id, total_cents
                    """,
                    (cart_id, total),
                ).fetchone()
            except errors.UniqueViolation as exc:
                raise Conflict(f"cart {cart_id} is already ordered") from exc
            except errors.CheckViolation as exc:
                raise Conflict(f"cart {cart_id} exceeds available stock") from exc
