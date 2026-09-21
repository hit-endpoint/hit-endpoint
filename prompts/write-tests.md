# Prompt: add tests to an existing request

```
Add tests to the hit request `<ref>` in `zones/<zone>`.

First run `hit -z zones/<zone> run <ref> --json` (or, if it must not be sent, ask me
for a sample response) and design tests from the actual response:
- the expected status
- types and presence of the fields a client would rely on (`type`, `exists`)
- one or two value checks that are stable across runs (avoid timestamps, random ids)
- a `max_ms` only if I mention a latency requirement
- for GraphQL, `errors: {exists: false}`

Use the `tests:` list format from `hit reference`, give related checks a `name:`, and keep the
list short. Re-run with `--json` to confirm every test passes, then show me the diff.
```
