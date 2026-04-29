package proposal

import (
	"encoding/json"
	"testing"
)

func TestDefaultOutputSchemas_AllPhasesPresent(t *testing.T) {
	tests := []struct {
		phase       string
		requiredKey string
	}{
		{"analysis", "options"},
		{"execution", "actionsTaken"},
		{"verification", "checks"},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			schema := defaultOutputSchemas[tt.phase]
			if schema == nil {
				t.Fatal("schema is nil")
			}

			var parsed map[string]any
			if err := json.Unmarshal(schema, &parsed); err != nil {
				t.Fatalf("schema is not valid JSON: %v", err)
			}

			props, ok := parsed["properties"].(map[string]any)
			if !ok {
				t.Fatal("schema has no properties")
			}
			if _, ok := props[tt.requiredKey]; !ok {
				t.Errorf("schema missing required key %q", tt.requiredKey)
			}
		})
	}
}

func TestDefaultOutputSchemas_UnknownPhaseReturnsNil(t *testing.T) {
	schema := defaultOutputSchemas["unknown"]
	if schema != nil {
		t.Error("unknown phase should return nil")
	}
}

func TestAnalysisOutputSchema_ValidJSON(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(AnalysisOutputSchema, &parsed); err != nil {
		t.Fatalf("AnalysisOutputSchema is not valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("type = %v, want object", parsed["type"])
	}
}

func TestExecutionOutputSchema_ValidJSON(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(ExecutionOutputSchema, &parsed); err != nil {
		t.Fatalf("ExecutionOutputSchema is not valid JSON: %v", err)
	}
	required, ok := parsed["required"].([]any)
	if !ok {
		t.Fatal("missing required field")
	}
	requiredSet := map[string]bool{}
	for _, r := range required {
		requiredSet[r.(string)] = true
	}
	if !requiredSet["actionsTaken"] {
		t.Error("ExecutionOutputSchema should require actionsTaken")
	}
}

func TestVerificationOutputSchema_ValidJSON(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(VerificationOutputSchema, &parsed); err != nil {
		t.Fatalf("VerificationOutputSchema is not valid JSON: %v", err)
	}
	required, ok := parsed["required"].([]any)
	if !ok {
		t.Fatal("missing required field")
	}
	requiredSet := map[string]bool{}
	for _, r := range required {
		requiredSet[r.(string)] = true
	}
	if !requiredSet["checks"] {
		t.Error("VerificationOutputSchema should require checks")
	}
}

func TestAnalysisOutputSchema_OptionsStructure(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(AnalysisOutputSchema, &parsed); err != nil {
		t.Fatal(err)
	}

	props := parsed["properties"].(map[string]any)
	options := props["options"].(map[string]any)

	if options["type"] != "array" {
		t.Error("options should be an array")
	}

	items := options["items"].(map[string]any)
	itemProps := items["properties"].(map[string]any)

	for _, key := range []string{"title", "diagnosis", "proposal", "rbac", "verification"} {
		if _, ok := itemProps[key]; !ok {
			t.Errorf("options items missing required property %q", key)
		}
	}

	required := items["required"].([]any)
	requiredSet := map[string]bool{}
	for _, r := range required {
		requiredSet[r.(string)] = true
	}
	for _, key := range []string{"title", "diagnosis", "proposal", "rbac", "verification"} {
		if !requiredSet[key] {
			t.Errorf("%q should be required in options items", key)
		}
	}
}

func TestVerificationOutputSchema_ChecksUseResultNotPassed(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(VerificationOutputSchema, &parsed); err != nil {
		t.Fatal(err)
	}

	props := parsed["properties"].(map[string]any)
	checks := props["checks"].(map[string]any)
	items := checks["items"].(map[string]any)
	itemProps := items["properties"].(map[string]any)

	if _, ok := itemProps["result"]; !ok {
		t.Error("checks items should have 'result' field (not 'passed')")
	}
	if _, ok := itemProps["passed"]; ok {
		t.Error("checks items should NOT have 'passed' field — use 'result' to match Go type")
	}

	resultField := itemProps["result"].(map[string]any)
	if resultField["type"] != "string" {
		t.Errorf("result type = %v, want string", resultField["type"])
	}

	required := items["required"].([]any)
	requiredSet := map[string]bool{}
	for _, r := range required {
		requiredSet[r.(string)] = true
	}
	if !requiredSet["result"] {
		t.Error("'result' should be required in checks items")
	}
}

func TestExecutionOutputSchema_ActionsUseOutcomeNotSuccess(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(ExecutionOutputSchema, &parsed); err != nil {
		t.Fatal(err)
	}

	props := parsed["properties"].(map[string]any)
	actions := props["actionsTaken"].(map[string]any)
	items := actions["items"].(map[string]any)
	itemProps := items["properties"].(map[string]any)

	if _, ok := itemProps["outcome"]; !ok {
		t.Error("actionsTaken items should have 'outcome' field (not 'success')")
	}
	if _, ok := itemProps["success"]; ok {
		t.Error("actionsTaken items should NOT have 'success' field — use 'outcome' to match Go type")
	}

	required := items["required"].([]any)
	requiredSet := map[string]bool{}
	for _, r := range required {
		requiredSet[r.(string)] = true
	}
	if !requiredSet["outcome"] {
		t.Error("'outcome' should be required in actionsTaken items")
	}
}
