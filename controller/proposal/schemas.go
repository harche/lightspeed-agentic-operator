package proposal

import "encoding/json"

// Default JSON Schemas sent to the agent for LLM structured output enforcement.
// Each phase has a known response shape. Components can override via
// Tools.OutputSchema in the Proposal spec.

var AnalysisOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "options": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "properties": {
          "title": { "type": "string" },
          "summary": { "type": "string" },
          "diagnosis": {
            "type": "object",
            "properties": {
              "summary": { "type": "string" },
              "confidence": { "type": "string", "enum": ["Low", "Medium", "High"] },
              "rootCause": { "type": "string" }
            },
            "required": ["summary", "confidence", "rootCause"]
          },
          "proposal": {
            "type": "object",
            "properties": {
              "description": { "type": "string" },
              "actions": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "type": { "type": "string" },
                    "description": { "type": "string" }
                  },
                  "required": ["type", "description"]
                }
              },
              "risk": { "type": "string", "enum": ["Low", "Medium", "High", "Critical"] },
              "reversible": { "type": "string", "enum": ["Reversible", "Irreversible", "Partial"] },
              "estimatedImpact": { "type": "string" },
              "rollbackPlan": {
                "type": "object",
                "properties": {
                  "description": { "type": "string" },
                  "command": { "type": "string" }
                },
                "required": ["description", "command"]
              }
            },
            "required": ["description", "actions", "risk", "reversible"]
          },
          "verification": {
            "type": "object",
            "properties": {
              "description": { "type": "string" },
              "steps": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "name": { "type": "string" },
                    "command": { "type": "string" },
                    "expected": { "type": "string" },
                    "type": { "type": "string" }
                  }
                }
              }
            }
          },
          "rbac": {
            "type": "object",
            "properties": {
              "namespaceScoped": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "namespace": { "type": "string" },
                    "apiGroups": { "type": "array", "items": { "type": "string", "minLength": 1 }, "description": "Use 'core' for the core API group (pods, services, configmaps, etc.)" },
                    "resources": { "type": "array", "items": { "type": "string" } },
                    "resourceNames": { "type": "array", "items": { "type": "string" } },
                    "verbs": { "type": "array", "items": { "type": "string" } },
                    "justification": { "type": "string" }
                  },
                  "required": ["apiGroups", "resources", "verbs", "justification"]
                }
              },
              "clusterScoped": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "apiGroups": { "type": "array", "items": { "type": "string", "minLength": 1 }, "description": "Use 'core' for the core API group (pods, services, configmaps, etc.)" },
                    "resources": { "type": "array", "items": { "type": "string" } },
                    "resourceNames": { "type": "array", "items": { "type": "string" } },
                    "verbs": { "type": "array", "items": { "type": "string" } },
                    "justification": { "type": "string" }
                  },
                  "required": ["apiGroups", "resources", "verbs", "justification"]
                }
              }
            }
          }
        },
        "required": ["title", "diagnosis", "proposal", "rbac", "verification"]
      }
    }
  },
  "required": ["options"]
}`)

var ExecutionOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "success": { "type": "boolean" },
    "actionsTaken": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "type": { "type": "string" },
          "description": { "type": "string" },
          "outcome": { "type": "string", "enum": ["Succeeded", "Failed"] },
          "output": { "type": "string" },
          "error": { "type": "string" }
        },
        "required": ["type", "description", "outcome"]
      }
    },
    "verification": {
      "type": "object",
      "properties": {
        "conditionOutcome": { "type": "string", "enum": ["Improved", "Unchanged", "Degraded"] },
        "summary": { "type": "string" }
      }
    }
  },
  "required": ["success", "actionsTaken"]
}`)

var VerificationOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "success": { "type": "boolean" },
    "checks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": { "type": "string" },
          "source": { "type": "string" },
          "value": { "type": "string" },
          "result": { "type": "string", "enum": ["Passed", "Failed"] }
        },
        "required": ["name", "result"]
      }
    },
    "summary": { "type": "string" }
  },
  "required": ["success", "checks", "summary"]
}`)

var EscalationOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "success": { "type": "boolean" },
    "summary": { "type": "string" },
    "content": { "type": "string" }
  },
  "required": ["success", "summary", "content"]
}`)

var defaultOutputSchemas = map[string]json.RawMessage{
	"analysis":     AnalysisOutputSchema,
	"execution":    ExecutionOutputSchema,
	"verification": VerificationOutputSchema,
	"escalation":   EscalationOutputSchema,
}
