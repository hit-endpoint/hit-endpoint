# Hit Endpoint

Go CLI (`hit`): fast, file-based API tester where API requests are YAML files under
`collections/`, run from the command line. Package source is `cmd/hit/` and `internal/`,
the YAML and command reference is `reference.md` (and embedded in `hit reference`),
and `examples/petstore-zone` and `examples/bandsintown-zone` plus `hit mock` form offline demos.

## Working on requests

Look at the collection's `_defaults.yaml` and a neighbour first, write one request per
file with tests and captures, run `hit sanity` when something seems off, `hit validate`,
`hit show`, then `hit run --json`. Never put secrets in committed files; never send write
requests or load tests at production unless asked for that specific call.

## Working on the tool

- Language: Go 1.22+
- Building: `go build -ldflags="-linkmode=external" -o hit ./cmd/hit && codesign -s - -f ./hit`
- Tests: `go test -ldflags="-linkmode=external" ./...`
- Dependencies are deliberately minimal: `gopkg.in/yaml.v3` and `github.com/jmespath/go-jmespath`.
- `reference.md` is the single source of truth for formats. When you change a YAML key or a command,
  update it, and the README if the change is user-facing.
