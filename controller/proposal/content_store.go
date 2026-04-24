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

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// ContentStore abstracts read/write access to content resources
// (request text, step results). Backed by the PostgreSQL instance
// provisioned by the lightspeed-operator (Deployment: lightspeed-postgres-server,
// Service: lightspeed-postgres-server:5432, credentials: Secret
// lightspeed-postgres-secret, namespace: openshift-lightspeed).
//
// The aggregated content API server and this interface share the same
// backing store — they are two access paths to the same data.
type ContentStore interface {
	// GetRequestContent reads the request payload for a proposal.
	GetRequestContent(ctx context.Context, name string) (*agenticv1alpha1.RequestContentSpec, error)

	// CreateRequestContent stores a request payload. Called by adapters
	// (AlertManager webhook, ACS adapter) when creating a proposal.
	CreateRequestContent(ctx context.Context, name string, spec agenticv1alpha1.RequestContentSpec) error

	// GetAnalysisResult reads the full analysis output.
	GetAnalysisResult(ctx context.Context, name string) (*agenticv1alpha1.AnalysisResultSpec, error)

	// CreateAnalysisResult stores analysis output after a successful analysis step.
	CreateAnalysisResult(ctx context.Context, name string, spec agenticv1alpha1.AnalysisResultSpec) error

	// GetExecutionResult reads the full execution output.
	GetExecutionResult(ctx context.Context, name string) (*agenticv1alpha1.ExecutionResultSpec, error)

	// CreateExecutionResult stores execution output after the execution step.
	CreateExecutionResult(ctx context.Context, name string, spec agenticv1alpha1.ExecutionResultSpec) error

	// GetVerificationResult reads the full verification output.
	GetVerificationResult(ctx context.Context, name string) (*agenticv1alpha1.VerificationResultSpec, error)

	// CreateVerificationResult stores verification output after the verification step.
	CreateVerificationResult(ctx context.Context, name string, spec agenticv1alpha1.VerificationResultSpec) error
}
