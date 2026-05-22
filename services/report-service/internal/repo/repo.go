// Package repo contains the pgx-backed report-service persistence
// layer plus embedded idempotent SQL migrations.
package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/report-service/internal/handlers"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies embedded SQL files in lexicographic order. The SQL is
// guarded by IF NOT EXISTS so it is safe to run on every start.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// Repo implements handlers.ReportStore with Postgres persistence.
type Repo struct{ Pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repo { return &Repo{Pool: pool} }

// Ping verifies the Postgres pool is reachable; it backs the /readyz probe.
func (r *Repo) Ping(ctx context.Context) error { return r.Pool.Ping(ctx) }

const definitionSelect = `SELECT id, name, description, owner, generator_kind, dataset_name,
	template, schedule, recipients, tags, parameters, active, last_generated_at, created_at, updated_at
	FROM report_definitions`

func (r *Repo) ListDefinitions(ctx context.Context) ([]handlers.ReportDefinition, error) {
	rows, err := r.Pool.Query(ctx, definitionSelect+` ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []handlers.ReportDefinition{}
	for rows.Next() {
		d, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) CreateDefinition(ctx context.Context, d handlers.ReportDefinition) (handlers.ReportDefinition, error) {
	template, schedule, recipients, tags, parameters, err := marshalDefinitionJSON(d)
	if err != nil {
		return handlers.ReportDefinition{}, err
	}
	row := r.Pool.QueryRow(ctx, `INSERT INTO report_definitions
		(id, name, description, owner, generator_kind, dataset_name, template, schedule, recipients, tags, parameters, active, last_generated_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, name, description, owner, generator_kind, dataset_name, template, schedule, recipients, tags, parameters, active, last_generated_at, created_at, updated_at`,
		d.ID, d.Name, d.Description, d.Owner, string(d.GeneratorKind), d.DatasetName, template, schedule, recipients, tags, parameters, d.Active, parseOptionalTime(d.LastGeneratedAt), parseTimeOrNow(d.CreatedAt), parseTimeOrNow(d.UpdatedAt))
	return scanDefinition(row)
}

func (r *Repo) GetDefinition(ctx context.Context, id string) (*handlers.ReportDefinition, error) {
	row := r.Pool.QueryRow(ctx, definitionSelect+` WHERE id = $1`, id)
	d, err := scanDefinition(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) UpdateDefinition(ctx context.Context, id string, patch map[string]json.RawMessage) (handlers.ReportDefinition, error) {
	d, err := r.GetDefinition(ctx, id)
	if err != nil {
		return handlers.ReportDefinition{}, err
	}
	if d == nil {
		return handlers.ReportDefinition{}, handlers.ErrNotFound
	}
	handlers.ApplyDefinitionPatch(d, patch)
	d.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	template, schedule, recipients, tags, parameters, err := marshalDefinitionJSON(*d)
	if err != nil {
		return handlers.ReportDefinition{}, err
	}
	row := r.Pool.QueryRow(ctx, `UPDATE report_definitions SET
		name=$2, description=$3, owner=$4, generator_kind=$5, dataset_name=$6,
		template=$7, schedule=$8, recipients=$9, tags=$10, parameters=$11,
		active=$12, updated_at=$13
		WHERE id=$1
		RETURNING id, name, description, owner, generator_kind, dataset_name, template, schedule, recipients, tags, parameters, active, last_generated_at, created_at, updated_at`,
		id, d.Name, d.Description, d.Owner, string(d.GeneratorKind), d.DatasetName, template, schedule, recipients, tags, parameters, d.Active, parseTimeOrNow(d.UpdatedAt))
	out, err := scanDefinition(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return handlers.ReportDefinition{}, handlers.ErrNotFound
	}
	return out, err
}

func (r *Repo) SaveExecution(ctx context.Context, e handlers.ReportExecution) error {
	preview, artifact, distributions, metrics, err := marshalExecutionJSON(e)
	if err != nil {
		return err
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, `INSERT INTO report_executions
		(id, report_id, report_name, status, generator_kind, triggered_by, generated_at, completed_at, preview, artifact, distributions, metrics)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		e.ID, e.ReportID, e.ReportName, e.Status, string(e.GeneratorKind), e.TriggeredBy, parseTimeOrNow(e.GeneratedAt), parseOptionalTime(e.CompletedAt), preview, artifact, distributions, metrics)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE report_definitions SET last_generated_at=$2, updated_at=$2 WHERE id=$1`, e.ReportID, parseTimeOrNow(e.GeneratedAt))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) ListExecutions(ctx context.Context, reportID string) ([]handlers.ReportExecution, error) {
	query := executionSelect + ` ORDER BY generated_at DESC`
	args := []any{}
	if reportID != "" {
		query = executionSelect + ` WHERE report_id = $1 ORDER BY generated_at DESC`
		args = append(args, reportID)
	}
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []handlers.ReportExecution{}
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repo) GetExecution(ctx context.Context, id string) (*handlers.ReportExecution, error) {
	row := r.Pool.QueryRow(ctx, executionSelect+` WHERE id = $1`, id)
	e, err := scanExecution(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

const executionSelect = `SELECT id, report_id, report_name, status, generator_kind, triggered_by,
	generated_at, completed_at, preview, artifact, distributions, metrics FROM report_executions`

type scanner interface{ Scan(...any) error }

func scanDefinition(row scanner) (handlers.ReportDefinition, error) {
	var d handlers.ReportDefinition
	var template, schedule, recipients, tags, parameters []byte
	var last *time.Time
	var created, updated time.Time
	var generatorKind string
	if err := row.Scan(&d.ID, &d.Name, &d.Description, &d.Owner, &generatorKind, &d.DatasetName, &template, &schedule, &recipients, &tags, &parameters, &d.Active, &last, &created, &updated); err != nil {
		return d, err
	}
	d.GeneratorKind = handlers.GeneratorKind(generatorKind)
	if err := unmarshal(template, &d.Template); err != nil {
		return d, err
	}
	if err := unmarshal(schedule, &d.Schedule); err != nil {
		return d, err
	}
	if err := unmarshal(recipients, &d.Recipients); err != nil {
		return d, err
	}
	if err := unmarshal(tags, &d.Tags); err != nil {
		return d, err
	}
	if err := unmarshal(parameters, &d.Parameters); err != nil {
		return d, err
	}
	if last != nil {
		s := last.UTC().Format(time.RFC3339Nano)
		d.LastGeneratedAt = &s
	}
	d.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	d.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	return d, nil
}

func scanExecution(row scanner) (handlers.ReportExecution, error) {
	var e handlers.ReportExecution
	var preview, artifact, distributions, metrics []byte
	var generated time.Time
	var completed *time.Time
	var generatorKind string
	if err := row.Scan(&e.ID, &e.ReportID, &e.ReportName, &e.Status, &generatorKind, &e.TriggeredBy, &generated, &completed, &preview, &artifact, &distributions, &metrics); err != nil {
		return e, err
	}
	e.GeneratorKind = handlers.GeneratorKind(generatorKind)
	if err := unmarshal(preview, &e.Preview); err != nil {
		return e, err
	}
	if err := unmarshal(artifact, &e.Artifact); err != nil {
		return e, err
	}
	if err := unmarshal(distributions, &e.Distributions); err != nil {
		return e, err
	}
	if err := unmarshal(metrics, &e.Metrics); err != nil {
		return e, err
	}
	e.GeneratedAt = generated.UTC().Format(time.RFC3339Nano)
	if completed != nil {
		s := completed.UTC().Format(time.RFC3339Nano)
		e.CompletedAt = &s
	}
	return e, nil
}

func marshalDefinitionJSON(d handlers.ReportDefinition) ([]byte, []byte, []byte, []byte, []byte, error) {
	template, err := json.Marshal(d.Template)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	schedule, err := json.Marshal(d.Schedule)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	recipients, err := json.Marshal(d.Recipients)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	tags, err := json.Marshal(d.Tags)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	parameters, err := json.Marshal(d.Parameters)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return template, schedule, recipients, tags, parameters, nil
}

func marshalExecutionJSON(e handlers.ReportExecution) ([]byte, []byte, []byte, []byte, error) {
	preview, err := json.Marshal(e.Preview)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	artifact, err := json.Marshal(e.Artifact)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	distributions, err := json.Marshal(e.Distributions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	metrics, err := json.Marshal(e.Metrics)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return preview, artifact, distributions, metrics, nil
}

func unmarshal(data []byte, dst any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func parseTimeOrNow(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}

func parseOptionalTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}
