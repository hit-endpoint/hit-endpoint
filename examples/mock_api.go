package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hit/internal/mock"
)

func main() {
	port := 8765
	var chaos mock.ChaosOptions
	var openapiSpec string
	stateless := false

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-p" || a == "--port") && i+1 < len(args):
			if val, err := strconv.Atoi(args[i+1]); err == nil && val > 0 {
				port = val
			}
			i++
		case strings.HasPrefix(a, "--port="):
			if val, err := strconv.Atoi(strings.TrimPrefix(a, "--port=")); err == nil && val > 0 {
				port = val
			}
		case (a == "--openapi" || a == "-o") && i+1 < len(args):
			openapiSpec = args[i+1]
			i++
		case strings.HasPrefix(a, "--openapi="):
			openapiSpec = strings.TrimPrefix(a, "--openapi=")
		case a == "--stateless":
			stateless = true
		case a == "--flaky" && i+1 < len(args):
			rate, _ := mock.ParseRate(args[i+1])
			chaos.FlakyRate = rate
			i++
		case strings.HasPrefix(a, "--flaky="):
			rate, _ := mock.ParseRate(strings.TrimPrefix(a, "--flaky="))
			chaos.FlakyRate = rate
		case a == "--flaky-status" && i+1 < len(args):
			code, _ := strconv.Atoi(args[i+1])
			chaos.FlakyStatus = code
			i++
		case strings.HasPrefix(a, "--flaky-status="):
			code, _ := strconv.Atoi(strings.TrimPrefix(a, "--flaky-status="))
			chaos.FlakyStatus = code
		case (a == "-r" || a == "--rate-limit") && i+1 < len(args):
			limit, window, _ := mock.ParseRateLimit(args[i+1])
			chaos.RateLimit = limit
			chaos.RateWindow = window
			i++
		case strings.HasPrefix(a, "--rate-limit="):
			limit, window, _ := mock.ParseRateLimit(strings.TrimPrefix(a, "--rate-limit="))
			chaos.RateLimit = limit
			chaos.RateWindow = window
		case a == "--latency" && i+1 < len(args):
			d, _ := time.ParseDuration(args[i+1])
			chaos.Latency = d
			i++
		case strings.HasPrefix(a, "--latency="):
			d, _ := time.ParseDuration(strings.TrimPrefix(a, "--latency="))
			chaos.Latency = d
		case a == "--jitter" && i+1 < len(args):
			minD, maxD, _ := mock.ParseJitter(args[i+1])
			chaos.JitterMin = minD
			chaos.JitterMax = maxD
			i++
		case strings.HasPrefix(a, "--jitter="):
			minD, maxD, _ := mock.ParseJitter(strings.TrimPrefix(a, "--jitter="))
			chaos.JitterMin = minD
			chaos.JitterMax = maxD
		case a == "--auth-expire" && i+1 < len(args):
			d, _ := time.ParseDuration(args[i+1])
			chaos.AuthExpire = d
			i++
		case strings.HasPrefix(a, "--auth-expire="):
			d, _ := time.ParseDuration(strings.TrimPrefix(a, "--auth-expire="))
			chaos.AuthExpire = d
		case a == "--corrupt" && i+1 < len(args):
			rate, _ := mock.ParseRate(args[i+1])
			chaos.CorruptRate = rate
			i++
		case strings.HasPrefix(a, "--corrupt="):
			rate, _ := mock.ParseRate(strings.TrimPrefix(a, "--corrupt="))
			chaos.CorruptRate = rate
		default:
			if !strings.HasPrefix(a, "-") {
				if p, err := strconv.Atoi(a); err == nil && p > 0 {
					port = p
				}
			}
		}
	}

	var server *mock.Server
	if openapiSpec != "" {
		var err error
		server, err = mock.NewOpenAPIServer(openapiSpec, chaos, !stateless)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load OpenAPI spec: %v\n", err)
			os.Exit(1)
		}
		engine := server.OpenAPIEngine()
		title := engine.Title()
		version := engine.Version()
		versionStr := ""
		if version != "" {
			versionStr = " " + version
		}
		fmt.Printf("hit dynamic openapi mock listening on http://127.0.0.1:%d  (Ctrl+C to stop)\n", port)
		fmt.Printf("📄 Specification: %s%s (%d routes loaded)\n", title, versionStr, engine.RouteCount())
		if !stateless {
			fmt.Printf("💾 State: in-memory CRUD enabled (use --stateless to disable)\n")
		} else {
			fmt.Printf("💾 State: stateless mode\n")
		}
	} else {
		server = mock.NewChaosServer(chaos)
		fmt.Printf("mock petstore listening on http://127.0.0.1:%d  (Ctrl+C to stop)\n", port)
	}

	if chaos.IsEnabled() {
		fmt.Printf("⚡ Chaos enabled: %s\n", chaos.Summary())
	}
	if err := server.ListenAndServe(port); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
