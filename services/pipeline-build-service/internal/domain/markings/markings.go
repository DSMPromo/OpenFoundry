// Package markings ports services/pipeline-build-service/src/domain/marking_propagation.rs
// 1:1: T3.4 propagation of markings from pipeline inputs to outputs.
//
// When a pipeline node finishes every marking present on any input
// must be inherited by the output dataset, recorded as
// `source = 'inherited_from_upstream'` with `inherited_from = <input_rid>`.
// Idempotency is delegated to the Postgres unique index on
// `dataset_markings (dataset_rid, marking_id, COALESCE(inherited_from, ”))`.
package markings

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PropagateToOutput inserts inherited rows on `dataset_markings` for
// `outputRID` covering every marking that exists on any of the
// `inputRIDs`. Returns the number of rows actually inserted (rows
// already present are silently skipped via ON CONFLICT DO NOTHING).
//
// Both direct and previously-inherited markings on each input
// propagate downstream — that's what keeps a 3-hop lineage chain
// (A → B → C) from silently dropping A's `RESTRICTED` at C.
func PropagateToOutput(
	ctx context.Context,
	pool *pgxpool.Pool,
	outputRID string,
	inputRIDs []string,
) (uint64, error) {
	if len(inputRIDs) == 0 {
		return 0, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var total uint64
	for _, inputRID := range inputRIDs {
		ct, err := tx.Exec(ctx, propagateSQL, outputRID, inputRID)
		if err != nil {
			return 0, err
		}
		total += uint64(ct.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return total, nil
}

// PropagateToOutputInTx is the variant that runs inside a caller-owned
// transaction — useful when the caller bundles the marking propagation
// with the row update that records the build outcome.
func PropagateToOutputInTx(
	ctx context.Context,
	tx pgx.Tx,
	outputRID string,
	inputRIDs []string,
) (uint64, error) {
	if len(inputRIDs) == 0 {
		return 0, nil
	}
	var total uint64
	for _, inputRID := range inputRIDs {
		ct, err := tx.Exec(ctx, propagateSQL, outputRID, inputRID)
		if err != nil {
			return 0, err
		}
		total += uint64(ct.RowsAffected())
	}
	return total, nil
}

const propagateSQL = `
INSERT INTO dataset_markings
       (dataset_rid, marking_id, source, inherited_from)
SELECT DISTINCT $1, marking_id, 'inherited_from_upstream', $2
  FROM dataset_markings
 WHERE dataset_rid = $2
ON CONFLICT DO NOTHING`
