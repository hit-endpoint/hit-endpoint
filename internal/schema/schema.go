package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

var RequestJSONSchema = map[string]any{
	"$schema":     "http://json-schema.org/draft-07/schema#",
	"title":       "HitRequestSpec",
	"description": "Schema for a hit-api-tester request file (.yaml)",
	"type":        "object",
	"required":    []string{"name", "method", "url"},
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "Human-readable name of the request",
		},
		"method": map[string]any{
			"type":        "string",
			"enum":        []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"},
			"description": "HTTP method",
		},
		"url": map[string]any{
			"type":        "string",
			"description": "Target URL (relative or absolute, supports {{variables}})",
		},
		"headers": map[string]any{
			"type":        "object",
			"description": "Key-value map of HTTP headers",
		},
		"query": map[string]any{
			"type":        "object",
			"description": "Key-value map of query parameters",
		},
		"auth": map[string]any{
			"description": "Authentication configuration ('inherit', 'none', or auth block)",
		},
		"body": map[string]any{
			"type":        "object",
			"description": "Request body: json, raw, form, multipart, or graphql",
			"properties": map[string]any{
				"json":      map[string]any{"description": "JSON body (object or array)"},
				"raw":       map[string]any{"type": "string", "description": "Raw text body"},
				"form":      map[string]any{"type": "object", "description": "Urlencoded form parameters"},
				"multipart": map[string]any{"type": "object", "description": "Multipart form fields/files"},
				"graphql":   map[string]any{"type": "object", "description": "GraphQL query and variables"},
			},
		},
		"matrix": map[string]any{
			"description": "Data-driven matrix testing rows (inline list, rows object, or external csv/json file)",
		},
		"timeout": map[string]any{
			"type":        "number",
			"description": "Timeout in seconds",
		},
		"vars": map[string]any{
			"type":        "object",
			"description": "Request-level default variables",
		},
		"tests": map[string]any{
			"type":        "array",
			"description": "List of assertions: status, max_ms, json, headers, expr",
		},
		"captures": map[string]any{
			"type":        "object",
			"description": "Response value captures (jmespath expressions)",
		},
		"hooks": map[string]any{
			"type":        "object",
			"description": "Lifecycle script hooks (before, after)",
		},
	},
}

var FlowJSONSchema = map[string]any{
	"$schema":     "http://json-schema.org/draft-07/schema#",
	"title":       "HitFlowSpec",
	"description": "Schema for a hit-api-tester scenario flow (.yaml)",
	"type":        "object",
	"required":    []string{"steps"},
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "Human-readable name of the scenario flow",
		},
		"vars": map[string]any{
			"type":        "object",
			"description": "Flow-level variables",
		},
		"steps": map[string]any{
			"type":        "array",
			"description": "Ordered list of flow execution steps",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"request":       map[string]any{"type": "string", "description": "Request reference path"},
					"set":           map[string]any{"type": "object", "description": "Assign variables"},
					"sleep":         map[string]any{"type": "number", "description": "Pause execution (seconds)"},
					"script":        map[string]any{"type": "string", "description": "Hook script to execute"},
					"flow":          map[string]any{"type": "string", "description": "Nested subflow reference"},
					"tests":         map[string]any{"type": "array", "description": "Additional test assertions for this step"},
					"replace_tests": map[string]any{"type": "array", "description": "Replace all assertions for this step"},
				},
			},
		},
	},
}

var ZoneJSONSchema = map[string]any{
	"$schema":     "http://json-schema.org/draft-07/schema#",
	"title":       "HitZoneConfig",
	"description": "Schema for zone.yaml",
	"type":        "object",
	"required":    []string{"default_environment"},
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "Zone name",
		},
		"default_environment": map[string]any{
			"type":        "string",
			"description": "Default environment (staging, dev, production, etc.)",
		},
		"vars": map[string]any{
			"type":        "object",
			"description": "Zone-wide global variables",
		},
	},
}

func GetSchema(kind string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(kind))
	var schemaObj any

	switch k {
	case "request", "req", "":
		schemaObj = RequestJSONSchema
	case "flow", "chain":
		schemaObj = FlowJSONSchema
	case "zone":
		schemaObj = ZoneJSONSchema
	default:
		return "", fmt.Errorf("unknown schema kind '%s' (supported: request, chain, zone)", kind)
	}

	b, err := json.MarshalIndent(schemaObj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
