CREATE TABLE orders (
    id TEXT PRIMARY KEY,
    state TEXT NOT NULL
);

CREATE TABLE refund_attempts (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES orders(id),
    gateway_receipt_id TEXT
);
