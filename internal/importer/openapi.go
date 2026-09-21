package importer

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"hit/internal/zone"
	"gopkg.in/yaml.v3"
)

var (
	pathParamRegex = regexp.MustCompile(`\{([a-zA-Z0-9_-]+)\}`)
	httpMethods    = []string{"get", "post", "put", "patch", "delete", "options", "head"}
)

func ImportOpenAPI(srcPath string, collectionsDir string, nameOverride string) (*ImportReport, error) {
	dataBytes, err := readSource(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read OpenAPI spec: %w", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(dataBytes, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI specification: %w", err)
	}

	isOpenAPI := false
	if v, ok := doc["openapi"].(string); ok && strings.HasPrefix(v, "3.") {
		isOpenAPI = true
	}
	isSwagger := false
	if v, ok := doc["swagger"].(string); ok && strings.HasPrefix(v, "2.") {
		isSwagger = true
	}

	if !isOpenAPI && !isSwagger {
		return nil, fmt.Errorf("%s: not a valid OpenAPI 3.x or Swagger 2.0 document", srcPath)
	}

	// 1. Determine collection name
	collName := nameOverride
	if collName == "" {
		if info, ok := doc["info"].(map[string]any); ok {
			if title, ok := info["title"].(string); ok && strings.TrimSpace(title) != "" {
				collName = strings.TrimSpace(title)
			}
		}
	}
	if collName == "" {
		base := filepath.Base(srcPath)
		collName = strings.TrimSuffix(base, filepath.Ext(base))
	}

	collSlug := Slugify(collName, "openapi-collection")
	destDir := filepath.Join(collectionsDir, collSlug)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	report := &ImportReport{
		Collection:  collName,
		Destination: destDir,
		Unconverted: make(map[string][]string),
	}

	// 2. Collection defaults (_defaults.yaml)
	defaults := make(map[string]any)
	defaults["headers"] = map[string]any{
		"Accept": "application/json",
	}

	// Base URL discovery
	var baseURL string
	if isOpenAPI {
		if servers, ok := doc["servers"].([]any); ok && len(servers) > 0 {
			if sm, ok := servers[0].(map[string]any); ok {
				if u, ok := sm["url"].(string); ok {
					baseURL = u
				}
			}
		}
	} else if isSwagger {
		host, _ := doc["host"].(string)
		basePath, _ := doc["basePath"].(string)
		scheme := "https"
		if schemes, ok := doc["schemes"].([]any); ok && len(schemes) > 0 {
			scheme = fmt.Sprintf("%v", schemes[0])
		}
		if host != "" {
			baseURL = fmt.Sprintf("%s://%s%s", scheme, host, basePath)
		}
	}

	if baseURL != "" {
		defaults["vars"] = map[string]any{
			"base_url": strings.TrimRight(baseURL, "/"),
		}
	}

	dYAML, err := zone.DumpYAML(defaults)
	if err == nil && len(defaults) > 0 {
		_ = os.WriteFile(filepath.Join(destDir, zone.DefaultsFile), []byte(dYAML), 0644)
	}

	// 3. Process paths
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return report, nil
	}

	var pathKeys []string
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	folderOrders := make(map[string]int)
	seenFolders := make(map[string]bool)

	for _, pathStr := range pathKeys {
		pathItem, ok := paths[pathStr].(map[string]any)
		if !ok {
			continue
		}

		pathParamsList, _ := pathItem["parameters"].([]any)

		for _, method := range httpMethods {
			opAny, hasMethod := pathItem[method]
			if !hasMethod {
				continue
			}
			op, ok := opAny.(map[string]any)
			if !ok {
				continue
			}

			// Tag / Folder determination
			folderDir := destDir
			tags, _ := op["tags"].([]any)
			if len(tags) > 0 && tags[0] != nil {
				tag := strings.TrimSpace(fmt.Sprintf("%v", tags[0]))
				if tag != "" {
					folderSlug := Slugify(tag, "endpoints")
					folderDir = filepath.Join(destDir, folderSlug)
					_ = os.MkdirAll(folderDir, 0755)
					if !seenFolders[folderDir] {
						seenFolders[folderDir] = true
						report.Folders++
					}
				}
			}

			order := folderOrders[folderDir] + 1
			folderOrders[folderDir] = order

			// Operation Name
			name := ""
			if summary, ok := op["summary"].(string); ok && strings.TrimSpace(summary) != "" {
				name = strings.TrimSpace(summary)
			} else if opID, ok := op["operationId"].(string); ok && strings.TrimSpace(opID) != "" {
				name = strings.TrimSpace(opID)
			} else {
				name = fmt.Sprintf("%s %s", strings.ToUpper(method), pathStr)
			}

			// Request file slug
			fileSlug := ""
			if opID, ok := op["operationId"].(string); ok && strings.TrimSpace(opID) != "" {
				fileSlug = Slugify(opID, "request")
			} else if summary, ok := op["summary"].(string); ok && strings.TrimSpace(summary) != "" {
				fileSlug = Slugify(summary, "request")
			} else {
				fileSlug = Slugify(method+"-"+pathStr, "request")
			}

			fileName := fmt.Sprintf("%02d-%s.yaml", order, fileSlug)
			filePath := filepath.Join(folderDir, fileName)

			// Build request specification
			reqSpec := make(map[string]any)
			reqSpec["name"] = name
			reqSpec["method"] = strings.ToUpper(method)

			// Convert /pets/{petId} -> {{base_url}}/pets/{{petId}}
			convertedPath := pathParamRegex.ReplaceAllString(pathStr, "{{$1}}")
			reqSpec["url"] = "{{base_url}}" + convertedPath

			// Merge parameters
			var allParams []any
			allParams = append(allParams, pathParamsList...)
			if opParams, ok := op["parameters"].([]any); ok {
				allParams = append(allParams, opParams...)
			}

			pathVars := make(map[string]any)
			queryParams := make(map[string]any)
			headerParams := make(map[string]any)

			for _, pAny := range allParams {
				pMap, ok := pAny.(map[string]any)
				if !ok {
					continue
				}
				if ref, ok := pMap["$ref"].(string); ok && ref != "" {
					if resolved := resolveRef(doc, ref); resolved != nil {
						pMap = resolved
					}
				}

				pName, _ := pMap["name"].(string)
				if pName == "" {
					continue
				}
				pIn, _ := pMap["in"].(string)

				schema := pMap["schema"]
				if schema == nil && isSwagger {
					schema = pMap
				}

				sampleVal := generateSample(doc, schema, 0)
				if sampleVal == nil {
					sampleVal = "example"
				}

				switch pIn {
				case "path":
					pathVars[pName] = sampleVal
				case "query":
					queryParams[pName] = sampleVal
				case "header":
					if !strings.EqualFold(pName, "Accept") && !strings.EqualFold(pName, "Content-Type") {
						headerParams[pName] = sampleVal
					}
				case "body": // Swagger 2.0 body param
					reqSpec["body"] = map[string]any{"json": sampleVal}
				}
			}

			if len(pathVars) > 0 {
				reqSpec["vars"] = pathVars
			}
			if len(queryParams) > 0 {
				reqSpec["query"] = queryParams
			}
			if len(headerParams) > 0 {
				reqSpec["headers"] = headerParams
			}

			// Request Body (OpenAPI 3.x)
			if reqBody, ok := op["requestBody"].(map[string]any); ok {
				if ref, ok := reqBody["$ref"].(string); ok && ref != "" {
					if resolved := resolveRef(doc, ref); resolved != nil {
						reqBody = resolved
					}
				}
				if content, ok := reqBody["content"].(map[string]any); ok {
					if jsonContent, ok := content["application/json"].(map[string]any); ok {
						bodySample := generateSample(doc, jsonContent["schema"], 0)
						if bodySample != nil {
							reqSpec["body"] = map[string]any{"json": bodySample}
						}
					} else if formContent, ok := content["application/x-www-form-urlencoded"].(map[string]any); ok {
						formSample := generateSample(doc, formContent["schema"], 0)
						if fm, ok := formSample.(map[string]any); ok {
							reqSpec["body"] = map[string]any{"form": fm}
						}
					} else if multiContent, ok := content["multipart/form-data"].(map[string]any); ok {
						multiSample := generateSample(doc, multiContent["schema"], 0)
						if mm, ok := multiSample.(map[string]any); ok {
							reqSpec["body"] = map[string]any{"multipart": mm}
						}
					}
				}
			}

			// Tests and Captures generation from responses
			var tests []any
			captures := make(map[string]any)

			if responses, ok := op["responses"].(map[string]any); ok {
				successCode := pickSuccessCode(responses)
				if successCode > 0 {
					tests = append(tests, map[string]any{"status": successCode})
					report.TestsConverted++
				}

				// Look for schema of success response
				respObjAny, hasResp := responses[strconv.Itoa(successCode)]
				if !hasResp {
					respObjAny = responses["200"]
				}
				if respObj, ok := respObjAny.(map[string]any); ok {
					if ref, ok := respObj["$ref"].(string); ok && ref != "" {
						if resolved := resolveRef(doc, ref); resolved != nil {
							respObj = resolved
						}
					}

					var respSchema any
					if isOpenAPI {
						if content, ok := respObj["content"].(map[string]any); ok {
							if jc, ok := content["application/json"].(map[string]any); ok {
								respSchema = jc["schema"]
							}
						}
					} else if isSwagger {
						respSchema = respObj["schema"]
					}

					if respSchema != nil {
						if ref, ok := respSchema.(map[string]any)["$ref"].(string); ok && ref != "" {
							if resolved := resolveRef(doc, ref); resolved != nil {
								respSchema = resolved
							}
						}

						if sm, ok := respSchema.(map[string]any); ok {
							// If object has required properties, generate test
							if reqProps, ok := sm["required"].([]any); ok && len(reqProps) > 0 {
								jsonChecks := make(map[string]any)
								for i, rp := range reqProps {
									if i >= 3 {
										break
									}
									propStr := fmt.Sprintf("%v", rp)
									jsonChecks[propStr] = map[string]any{"exists": true}
								}
								if len(jsonChecks) > 0 {
									tests = append(tests, map[string]any{"json": jsonChecks})
									report.TestsConverted++
								}
							}

							// Auto-capture ID or token
							if props, ok := sm["properties"].(map[string]any); ok {
								for _, candidate := range []string{"id", "token", "access_token"} {
									if _, hasProp := props[candidate]; hasProp {
										capName := fileSlug + "_" + candidate
										captures[capName] = "json." + candidate
										report.CapturesConverted++
										break
									}
								}
							}
						}
					}
				}
			}

			if len(tests) > 0 {
				reqSpec["tests"] = tests
			}
			if len(captures) > 0 {
				reqSpec["captures"] = captures
			}

			yStr, err := zone.DumpYAML(reqSpec)
			if err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: failed to serialize YAML: %v", name, err))
				continue
			}

			if err := os.WriteFile(filePath, []byte(yStr), 0644); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: failed to write file: %v", filePath, err))
				continue
			}

			report.Files = append(report.Files, filePath)
			report.Requests++
		}
	}

	return report, nil
}

func readSource(src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		resp, err := http.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(src)
}

func resolveRef(doc map[string]any, ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	parts := strings.Split(ref[2:], "/")
	var current any = doc
	for _, p := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[p]
	}
	if res, ok := current.(map[string]any); ok {
		return res
	}
	return nil
}

func generateSample(doc map[string]any, schema any, depth int) any {
	if depth > 5 || schema == nil {
		return nil
	}
	sm, ok := schema.(map[string]any)
	if !ok {
		return nil
	}

	if ref, ok := sm["$ref"].(string); ok && ref != "" {
		if resolved := resolveRef(doc, ref); resolved != nil {
			return generateSample(doc, resolved, depth+1)
		}
	}

	if ex, ok := sm["example"]; ok && ex != nil {
		return ex
	}
	if def, ok := sm["default"]; ok && def != nil {
		return def
	}

	sType, _ := sm["type"].(string)
	switch sType {
	case "string":
		format, _ := sm["format"].(string)
		if format == "date-time" {
			return "2026-09-09T00:00:00Z"
		}
		if format == "date" {
			return "2026-09-09"
		}
		if format == "uuid" {
			return "{{$uuid}}"
		}
		if format == "email" {
			return "user@example.com"
		}
		if enums, ok := sm["enum"].([]any); ok && len(enums) > 0 {
			return enums[0]
		}
		return "string"
	case "integer":
		return 1
	case "number":
		return 1.0
	case "boolean":
		return true
	case "array":
		if items := sm["items"]; items != nil {
			itemSample := generateSample(doc, items, depth+1)
			if itemSample != nil {
				return []any{itemSample}
			}
		}
		return []any{}
	case "object":
		props, ok := sm["properties"].(map[string]any)
		if !ok {
			return make(map[string]any)
		}
		obj := make(map[string]any)
		for propName, propSchema := range props {
			val := generateSample(doc, propSchema, depth+1)
			if val != nil {
				obj[propName] = val
			} else {
				obj[propName] = "value"
			}
		}
		return obj
	default:
		if props, ok := sm["properties"].(map[string]any); ok {
			obj := make(map[string]any)
			for propName, propSchema := range props {
				obj[propName] = generateSample(doc, propSchema, depth+1)
			}
			return obj
		}
		return "value"
	}
}

func pickSuccessCode(responses map[string]any) int {
	for _, codeStr := range []string{"200", "201", "204", "202"} {
		if _, ok := responses[codeStr]; ok {
			if c, err := strconv.Atoi(codeStr); err == nil {
				return c
			}
		}
	}
	var codes []int
	for k := range responses {
		if c, err := strconv.Atoi(k); err == nil && c >= 200 && c < 400 {
			codes = append(codes, c)
		}
	}
	if len(codes) > 0 {
		sort.Ints(codes)
		return codes[0]
	}
	return 200
}
