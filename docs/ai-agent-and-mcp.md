# AI Coding Agent Tooling & MCP Server

`hit` is designed as a first-class tool for autonomous AI coding agents (such as Claude Desktop, Cursor, Windsurf, GitHub Copilot, and custom LLM agents). It bridges the gap between AI code generation and deterministic API execution.

---

## Model Context Protocol (MCP) Stdio Server (`hit mcp`)

`hit` includes a built-in stdio [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server that allows AI assistants to inspect API zones, execute requests, load test endpoints, and evaluate variables as native tools.

### Configuration

Add `hit mcp` to your MCP client configuration (e.g. `claude_desktop_config.json` or `~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "hit": {
      "command": "/usr/local/bin/hit",
      "args": ["-w", "/path/to/your/zone", "mcp"]
    }
  }
}
```

### Exposed Native Tools

| Tool | Purpose |
|---|---|
| `hit_sanity` | Evaluates zone readiness, environment configuration, and server reachability |
| `hit_list` | Discovers and catalogs all available requests, scenario chains, and servers |
| `hit_show` | Renders a request specification with resolved variables without sending |
| `hit_run` | Executes requests or scenario flows with live assertion evaluations and overrides |
| `hit_perf` | Executes high-concurrency benchmarks with SLA/SLO threshold validation |
| `hit_vars` | Inspects and manages zone, environment, and captured runtime state |

---

## Token-Compact Agent Output (`--format=agent`)

When running CLI commands within an agent feedback loop, ANSI color escapes and multi-kilobyte JSON payloads waste context tokens. The `--format=agent` flag emits a concise, token-efficient summary designed specifically for LLM parsers:

```bash
hit run petstore/pets/list --format=agent
```

Output:
```
PASS petstore/pets/01-list (200 OK, 12ms) [7/7 passed]
SUMMARY: ALL 1 REQUESTS PASSED
```

On failure, it outputs the exact file path, failing assertion expression, and expected vs actual values:
```
FAIL petstore/pets/01-list (200 OK, 14ms) [6/7 passed]
  ✗ json.items[0].id: expected type integer, got string ("123") [file: collections/pets/01-list.yaml:28]
SUMMARY: 1 OF 1 REQUESTS FAILED
```

---

## JSON Schemas (`hit schema`)

`hit` exports standard Draft-07 JSON Schemas for zone configuration, scenario flows, and request files. Use these for editor autocomplete (e.g. VS Code YAML schema mapping) or LLM structured output generation:

```bash
# Export request schema
hit schema request > hit-request.schema.json

# Export scenario flow schema
hit schema flow > hit-flow.schema.json

# Export zone schema
hit schema zone > hit-zone.schema.json
```

---

## Prompts Library (`prompts/`)

The repository includes a curated collection of ready-to-use prompt templates under `prompts/`:
* `prompts/new-request.md`: Instructions for scaffolding clean REST request YAML files.
* `prompts/new-graphql-request.md`: Prompt for creating GraphQL queries, mutations, and variables.
* `prompts/new-flow.md`: Prompt for designing chained scenario flows.
* `prompts/debug-request.md`: Prompt for debugging failing assertions and analyzing drift.
* `prompts/perf-test.md`: Prompt for configuring load benchmarks and SLA threshold gates.
