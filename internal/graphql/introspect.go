package graphql

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StandardIntrospectionQuery queries the GraphQL server __schema metadata.
const StandardIntrospectionQuery = `
query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind
      name
      description
      fields(includeDeprecated: true) {
        name
        description
        args {
          name
          description
          type { ...TypeRef }
          defaultValue
        }
        type { ...TypeRef }
        isDeprecated
        deprecationReason
      }
      inputFields {
        name
        description
        type { ...TypeRef }
        defaultValue
      }
      interfaces { ...TypeRef }
      enumValues(includeDeprecated: true) {
        name
        description
        isDeprecated
        deprecationReason
      }
      possibleTypes { ...TypeRef }
    }
  }
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
      ofType {
        kind
        name
        ofType {
          kind
          name
        }
      }
    }
  }
}
`

// SchemaType models a GraphQL type from introspection.
type SchemaType struct {
	Kind          string        `json:"kind"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Fields        []SchemaField `json:"fields,omitempty"`
	InputFields   []SchemaField `json:"inputFields,omitempty"`
	EnumValues    []EnumValue   `json:"enumValues,omitempty"`
	PossibleTypes []TypeRef     `json:"possibleTypes,omitempty"`
	Interfaces    []TypeRef     `json:"interfaces,omitempty"`
}

// SchemaField models a field on a GraphQL type.
type SchemaField struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Args        []FieldArg `json:"args,omitempty"`
	Type        TypeRef    `json:"type"`
}

// FieldArg models an argument on a field.
type FieldArg struct {
	Name         string  `json:"name"`
	Description  string  `json:"description,omitempty"`
	Type         TypeRef `json:"type"`
	DefaultValue *string `json:"defaultValue,omitempty"`
}

// EnumValue models an enum value.
type EnumValue struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// TypeRef models a type reference including modifiers (NON_NULL, LIST).
type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name,omitempty"`
	OfType *TypeRef `json:"ofType,omitempty"`
}

// Format returns the SDL representation of the type reference (e.g. "[String!]!").
func (t TypeRef) Format() string {
	switch t.Kind {
	case "NON_NULL":
		if t.OfType != nil {
			return t.OfType.Format() + "!"
		}
		return "Any!"
	case "LIST":
		if t.OfType != nil {
			return "[" + t.OfType.Format() + "]"
		}
		return "[Any]"
	default:
		if t.Name != nil {
			return *t.Name
		}
		return "Any"
	}
}

// IntrospectionSchema models the top-level introspection output.
type IntrospectionSchema struct {
	QueryType        *struct{ Name string } `json:"queryType"`
	MutationType     *struct{ Name string } `json:"mutationType"`
	SubscriptionType *struct{ Name string } `json:"subscriptionType"`
	Types            []SchemaType           `json:"types"`
}

// IntrospectSchema queries the GraphQL endpoint and returns the parsed IntrospectionSchema.
func IntrospectSchema(url string, headers map[string]string, timeout time.Duration, insecure bool) (*IntrospectionSchema, error) {
	res, err := Execute(ExecuteOptions{
		URL:      url,
		Query:    StandardIntrospectionQuery,
		Headers:  headers,
		Timeout:  timeout,
		Insecure: insecure,
	})
	if err != nil {
		return nil, err
	}
	if res.HasErrors() {
		return nil, fmt.Errorf("introspection failed:\n%s", res.FormatErrors())
	}

	dataMap, ok := res.Data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected introspection response data format")
	}

	schemaJSON, err := json.Marshal(dataMap["__schema"])
	if err != nil {
		return nil, fmt.Errorf("failed to re-encode schema: %w", err)
	}

	var schema IntrospectionSchema
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse introspection schema: %w", err)
	}

	return &schema, nil
}

// Introspect sends an introspection query to the target URL and returns Schema SDL.
func Introspect(url string, headers map[string]string, timeout time.Duration, insecure bool) (string, error) {
	schema, err := IntrospectSchema(url, headers, timeout, insecure)
	if err != nil {
		return "", err
	}
	return FormatSDL(*schema), nil
}

// FormatSDL converts an IntrospectionSchema into standard GraphQL Schema Definition Language (SDL).
func FormatSDL(schema IntrospectionSchema) string {
	var sb strings.Builder
	sb.WriteString("# --- GraphQL Schema Definition Language (SDL) ---\n")
	sb.WriteString("# Generated automatically by hit-api-tester\n\n")

	// Schema declaration
	var rootOps []string
	if schema.QueryType != nil && schema.QueryType.Name != "" {
		rootOps = append(rootOps, fmt.Sprintf("  query: %s", schema.QueryType.Name))
	}
	if schema.MutationType != nil && schema.MutationType.Name != "" {
		rootOps = append(rootOps, fmt.Sprintf("  mutation: %s", schema.MutationType.Name))
	}
	if schema.SubscriptionType != nil && schema.SubscriptionType.Name != "" {
		rootOps = append(rootOps, fmt.Sprintf("  subscription: %s", schema.SubscriptionType.Name))
	}
	if len(rootOps) > 0 {
		sb.WriteString("schema {\n")
		sb.WriteString(strings.Join(rootOps, "\n") + "\n}\n\n")
	}

	// Filter out internal built-ins (__Schema, __Type, etc.) and standard scalars
	var customTypes []SchemaType
	for _, t := range schema.Types {
		if strings.HasPrefix(t.Name, "__") {
			continue
		}
		if t.Kind == "SCALAR" {
			switch t.Name {
			case "Int", "Float", "String", "Boolean", "ID":
				continue
			}
		}
		customTypes = append(customTypes, t)
	}

	sort.Slice(customTypes, func(i, j int) bool {
		return customTypes[i].Name < customTypes[j].Name
	})

	for _, t := range customTypes {
		if t.Description != "" {
			sb.WriteString(fmt.Sprintf("\"\"\"%s\"\"\"\n", t.Description))
		}
		switch t.Kind {
		case "SCALAR":
			sb.WriteString(fmt.Sprintf("scalar %s\n\n", t.Name))

		case "ENUM":
			sb.WriteString(fmt.Sprintf("enum %s {\n", t.Name))
			for _, ev := range t.EnumValues {
				if ev.Description != "" {
					sb.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", ev.Description))
				}
				sb.WriteString(fmt.Sprintf("  %s\n", ev.Name))
			}
			sb.WriteString("}\n\n")

		case "INPUT_OBJECT":
			sb.WriteString(fmt.Sprintf("input %s {\n", t.Name))
			for _, ifield := range t.InputFields {
				if ifield.Description != "" {
					sb.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", ifield.Description))
				}
				sb.WriteString(fmt.Sprintf("  %s: %s\n", ifield.Name, ifield.Type.Format()))
			}
			sb.WriteString("}\n\n")

		case "OBJECT", "INTERFACE":
			keyword := "type"
			if t.Kind == "INTERFACE" {
				keyword = "interface"
			}
			implements := ""
			if len(t.Interfaces) > 0 {
				var ifaceNames []string
				for _, iface := range t.Interfaces {
					ifaceNames = append(ifaceNames, iface.Format())
				}
				implements = " implements " + strings.Join(ifaceNames, " & ")
			}
			sb.WriteString(fmt.Sprintf("%s %s%s {\n", keyword, t.Name, implements))
			for _, f := range t.Fields {
				if f.Description != "" {
					sb.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", f.Description))
				}
				argsStr := ""
				if len(f.Args) > 0 {
					var argParts []string
					for _, a := range f.Args {
						argParts = append(argParts, fmt.Sprintf("%s: %s", a.Name, a.Type.Format()))
					}
					argsStr = "(" + strings.Join(argParts, ", ") + ")"
				}
				sb.WriteString(fmt.Sprintf("  %s%s: %s\n", f.Name, argsStr, f.Type.Format()))
			}
			sb.WriteString("}\n\n")

		case "UNION":
			var possible []string
			for _, pt := range t.PossibleTypes {
				possible = append(possible, pt.Format())
			}
			sb.WriteString(fmt.Sprintf("union %s = %s\n\n", t.Name, strings.Join(possible, " | ")))
		}
	}

	return strings.TrimSpace(sb.String())
}
