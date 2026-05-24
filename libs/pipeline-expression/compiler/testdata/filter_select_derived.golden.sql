CREATE OR REPLACE TEMP VIEW node_96879611650f AS
SELECT *
FROM node_25a6634263c1
WHERE status = 'ACTIVE' AND amount > 0;

CREATE OR REPLACE TEMP VIEW node_5eda958e9dde AS
SELECT `order_id`, `customer_id`, `amount`, `placed_at`
FROM node_96879611650f;

CREATE OR REPLACE TEMP VIEW node_b64c161a4f84 AS
SELECT *, (amount * 100) AS `amount_cents`, (to_date(placed_at)) AS `placed_date`
FROM node_5eda958e9dde;

INSERT OVERWRITE {{output:out}}
SELECT * FROM node_b64c161a4f84;
