CREATE OR REPLACE TEMP VIEW node_ec654fac9599 AS
SELECT *
FROM node_2feeeb7a3bf7
WHERE event_id IS NOT NULL;

INSERT INTO {{output:events_out}}
SELECT * FROM node_ec654fac9599;
