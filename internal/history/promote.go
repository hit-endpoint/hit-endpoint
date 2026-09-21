package history

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"hit/internal/assertions"
)

// PromoteToYAML converts an Entry to a structured hit request YAML string
func PromoteToYAML(entry *Entry, targetName string) (string, error) {
	return PromoteToYAMLWithInference(entry, targetName, false)
}

// PromoteToYAMLWithInference converts an Entry to a structured hit request YAML string with optional test inference
func PromoteToYAMLWithInference(entry *Entry, targetName string, infer bool) (string, error) {
	if entry == nil {
		return "", fmt.Errorf("entry is nil")
	}

	name := targetName
	if name == "" {
		name = entry.Ref
	}
	if name == "" {
		if parsed, err := url.Parse(entry.Url); err == nil && parsed.Path != "" {
			name = strings.Trim(strings.ReplaceAll(parsed.Path, "/", " "), " ")
		}
	}
	if name == "" {
		name = fmt.Sprintf("%s request", entry.Method)
	}

	reqMap := yaml.Node{
		Kind: yaml.MappingNode,
	}

	addKV := func(key string, valNode *yaml.Node) {
		reqMap.Content = append(reqMap.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			valNode,
		)
	}
	addScalar := func(key, val string) {
		addKV(key, &yaml.Node{Kind: yaml.ScalarNode, Value: val})
	}

	addScalar("name", name)
	if entry.Method != "" && entry.Method != "GET" {
		addScalar("method", entry.Method)
	}
	addScalar("url", entry.Url)

	// Filter out ephemeral/masked headers
	headersNode := &yaml.Node{Kind: yaml.MappingNode}
	for k, v := range entry.Headers {
		lk := strings.ToLower(k)
		if lk == "content-length" || lk == "host" || lk == "user-agent" || v == "[MASKED]" {
			continue
		}
		headersNode.Content = append(headersNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: v},
		)
	}
	if len(headersNode.Content) > 0 {
		addKV("headers", headersNode)
	}

	// Body formatting
	if entry.Body != "" {
		bodyNode := &yaml.Node{Kind: yaml.MappingNode}
		var parsedJSON any
		if err := json.Unmarshal([]byte(entry.Body), &parsedJSON); err == nil {
			var jsonNode yaml.Node
			data, _ := yaml.Marshal(parsedJSON)
			if err := yaml.Unmarshal(data, &jsonNode); err == nil && len(jsonNode.Content) > 0 {
				bodyNode.Content = append(bodyNode.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "json"},
					jsonNode.Content[0],
				)
			} else {
				bodyNode.Content = append(bodyNode.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "raw"},
					&yaml.Node{Kind: yaml.ScalarNode, Value: entry.Body},
				)
			}
		} else {
			bodyNode.Content = append(bodyNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "raw"},
				&yaml.Node{Kind: yaml.ScalarNode, Value: entry.Body},
			)
		}
		addKV("body", bodyNode)
	}

	// Tests
	if infer {
		tests, _, err := assertions.InferFromResponse(
			entry.Status,
			entry.ElapsedMs,
			entry.ResponseHeaders,
			entry.ResponseBody,
			assertions.DefaultInferOptions(),
		)
		if err == nil && len(tests) > 0 {
			data, err := yaml.Marshal(tests)
			if err == nil {
				var inferredNode yaml.Node
				if err := yaml.Unmarshal(data, &inferredNode); err == nil && len(inferredNode.Content) > 0 {
					addKV("tests", inferredNode.Content[0])
				}
			}
		}
	}

	// Fallback if tests were not added by inference
	hasTests := false
	for i := 0; i < len(reqMap.Content); i += 2 {
		if reqMap.Content[i].Value == "tests" {
			hasTests = true
			break
		}
	}
	if !hasTests {
		testsNode := &yaml.Node{Kind: yaml.SequenceNode}
		expectedStatus := 200
		if entry.Status > 0 {
			expectedStatus = entry.Status
		}
		statusTest := &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "status"},
				{Kind: yaml.ScalarNode, Value: fmt.Sprint(expectedStatus), Tag: "!!int"},
			},
		}
		testsNode.Content = append(testsNode.Content, statusTest)
		addKV("tests", testsNode)
	}

	outDoc := yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{&reqMap},
	}

	data, err := yaml.Marshal(&outDoc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveAsRequest writes an Entry as a request YAML file
func SaveAsRequest(entry *Entry, targetPath string) error {
	return SaveAsRequestWithInference(entry, targetPath, false)
}

// SaveAsRequestWithInference writes an Entry as a request YAML file with optional test inference
func SaveAsRequestWithInference(entry *Entry, targetPath string, infer bool) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	name := strings.TrimSuffix(filepath.Base(targetPath), filepath.Ext(targetPath))
	content, err := PromoteToYAMLWithInference(entry, name, infer)
	if err != nil {
		return err
	}

	return os.WriteFile(targetPath, []byte(content), 0644)
}
