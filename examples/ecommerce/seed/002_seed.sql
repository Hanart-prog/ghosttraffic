-- Deterministic synthetic catalog: rows are pure functions of the series value.
-- Do not use random() or now() here; replay fairness depends on identical state.
INSERT INTO products (id, sku, name, category, price_cents, stock)
SELECT g,
       format('SKU-%s', lpad(g::text, 5, '0')),
       format('%s %s', (ARRAY['Trail Shoe', 'Rain Jacket', 'Wool Sock', 'Day Pack'])[g % 4 + 1], g),
       (ARRAY['shoes', 'outerwear', 'socks', 'bags'])[g % 4 + 1],
       500 + (g * 37) % 19500,
       100 + (g * 13) % 400
FROM generate_series(1, 2000) AS g;

INSERT INTO customers (id, email)
SELECT g, format('customer-%s@example.test', lpad(g::text, 4, '0'))
FROM generate_series(1, 100) AS g;

SELECT setval(pg_get_serial_sequence('products', 'id'), 2000);
SELECT setval(pg_get_serial_sequence('customers', 'id'), 100);
