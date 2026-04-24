/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proposal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// contentTable is a typed string restricting table names to a closed set.
type contentTable string

const (
	tableRequestContent      contentTable = "request_content"
	tableAnalysisResults     contentTable = "analysis_results"
	tableExecutionResults    contentTable = "execution_results"
	tableVerificationResults contentTable = "verification_results"

	// maxSpecBytes is the maximum size of a JSONB spec column (10 MB).
	maxSpecBytes = 10 * 1024 * 1024
)

// PostgresContentStore implements ContentStore backed by PostgreSQL.
// In production, this connects to the PostgreSQL instance provisioned
// by the lightspeed-operator (lightspeed-postgres-server:5432 in the
// openshift-lightspeed namespace).
//
// Binary data (ContentPayload.Data) is stored in a separate BYTEA
// column to avoid base64 bloat in JSONB. The JSONB spec column holds
// structured metadata and text content only.
type PostgresContentStore struct {
	db *sql.DB
}

// NewPostgresContentStore creates a PostgresContentStore and ensures
// the required tables exist. The caller owns the *sql.DB lifecycle.
func NewPostgresContentStore(db *sql.DB) (*PostgresContentStore, error) {
	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("failed to create content store tables: %w", err)
	}
	return &PostgresContentStore{db: db}, nil
}

func createTables(db *sql.DB) error {
	tables := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS request_content (
			name TEXT PRIMARY KEY,
			spec JSONB NOT NULL CHECK (octet_length(spec::text) <= %d),
			data BYTEA
		)`, maxSpecBytes),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS analysis_results (
			name TEXT PRIMARY KEY,
			spec JSONB NOT NULL CHECK (octet_length(spec::text) <= %d),
			data BYTEA
		)`, maxSpecBytes),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS execution_results (
			name TEXT PRIMARY KEY,
			spec JSONB NOT NULL CHECK (octet_length(spec::text) <= %d),
			data BYTEA
		)`, maxSpecBytes),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS verification_results (
			name TEXT PRIMARY KEY,
			spec JSONB NOT NULL CHECK (octet_length(spec::text) <= %d),
			data BYTEA
		)`, maxSpecBytes),
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("exec %q: %w", ddl[:40], err)
		}
	}
	return nil
}

func (s *PostgresContentStore) GetRequestContent(ctx context.Context, name string) (*agenticv1alpha1.RequestContentSpec, error) {
	var spec agenticv1alpha1.RequestContentSpec
	if err := getWithData(ctx, s.db, tableRequestContent, name, &spec, &spec.Data); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *PostgresContentStore) CreateRequestContent(ctx context.Context, name string, spec agenticv1alpha1.RequestContentSpec) error {
	data := spec.Data
	spec.Data = nil
	return upsertWithData(ctx, s.db, tableRequestContent, name, data, spec)
}

func (s *PostgresContentStore) GetAnalysisResult(ctx context.Context, name string) (*agenticv1alpha1.AnalysisResultSpec, error) {
	var spec agenticv1alpha1.AnalysisResultSpec
	if err := getWithData(ctx, s.db, tableAnalysisResults, name, &spec, &spec.Data); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *PostgresContentStore) CreateAnalysisResult(ctx context.Context, name string, spec agenticv1alpha1.AnalysisResultSpec) error {
	data := spec.Data
	spec.Data = nil
	return upsertWithData(ctx, s.db, tableAnalysisResults, name, data, spec)
}

func (s *PostgresContentStore) GetExecutionResult(ctx context.Context, name string) (*agenticv1alpha1.ExecutionResultSpec, error) {
	var spec agenticv1alpha1.ExecutionResultSpec
	if err := getWithData(ctx, s.db, tableExecutionResults, name, &spec, &spec.Data); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *PostgresContentStore) CreateExecutionResult(ctx context.Context, name string, spec agenticv1alpha1.ExecutionResultSpec) error {
	data := spec.Data
	spec.Data = nil
	return upsertWithData(ctx, s.db, tableExecutionResults, name, data, spec)
}

func (s *PostgresContentStore) GetVerificationResult(ctx context.Context, name string) (*agenticv1alpha1.VerificationResultSpec, error) {
	var spec agenticv1alpha1.VerificationResultSpec
	if err := getWithData(ctx, s.db, tableVerificationResults, name, &spec, &spec.Data); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *PostgresContentStore) CreateVerificationResult(ctx context.Context, name string, spec agenticv1alpha1.VerificationResultSpec) error {
	data := spec.Data
	spec.Data = nil
	return upsertWithData(ctx, s.db, tableVerificationResults, name, data, spec)
}

// upsertWithData marshals the spec to JSONB and stores the binary
// payload as BYTEA. Callers must nil out spec.Data before calling —
// this ensures the JSONB column contains no base64-encoded binary.
//
//	data := spec.Data   // save the 5MB PNG
//	spec.Data = nil     // json.Marshal will now skip the "data" field
//	upsertWithData(..., data, spec)
//	//                   ↓      ↓
//	//                BYTEA   JSONB (clean, no base64)
func upsertWithData(ctx context.Context, db *sql.DB, table contentTable, name string, binaryData []byte, spec interface{}) error {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	query := fmt.Sprintf(
		"INSERT INTO %s (name, spec, data) VALUES ($1, $2, $3) ON CONFLICT (name) DO UPDATE SET spec = $2, data = $3",
		table)
	_, err = db.ExecContext(ctx, query, name, specJSON, binaryData)
	return err
}

// getWithData reads the JSONB spec and BYTEA data columns, unmarshals
// the spec, and sets the binary data on the provided pointer.
func getWithData(ctx context.Context, db *sql.DB, table contentTable, name string, dest interface{}, dataOut *[]byte) error {
	var specJSON []byte
	var binaryData []byte

	query := fmt.Sprintf("SELECT spec, data FROM %s WHERE name = $1", table)
	err := db.QueryRowContext(ctx, query, name).Scan(&specJSON, &binaryData)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s %q not found", table, name)
	}
	if err != nil {
		return err
	}

	if err := json.Unmarshal(specJSON, dest); err != nil {
		return err
	}
	*dataOut = binaryData
	return nil
}
