"""Database access shared by the backend variants."""

import os

import psycopg

# Hashes the seeded tables in a fixed order, so equal digests across backends
# are evidence of equal initial state. Orders change stock, and so the digest.
SEED_DIGEST_SQL = """
SELECT md5(
    (SELECT string_agg(concat_ws('|', id, sku, name, category, price_cents, stock),
                       E'\\n' ORDER BY id) FROM products)
    || (SELECT string_agg(concat_ws('|', id, email), E'\\n' ORDER BY id) FROM customers)
)
"""


def seed_digest() -> str:
    with psycopg.connect(os.environ["DATABASE_URL"], connect_timeout=2) as conn:
        row = conn.execute(SEED_DIGEST_SQL).fetchone()
    if row is None or row[0] is None:
        raise RuntimeError("database is not seeded")
    return row[0]
