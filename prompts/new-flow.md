# Prompt: chain requests into a flow

```
Create a flow `zones/<zone>/flows/<name>.yaml` that runs these steps in order:

1. <e.g. auth/login>
2. <e.g. create a thing, capturing its id>
3. <e.g. fetch it, then delete it, then confirm 404>

Rules: reuse existing request files (`hit ls --json` shows them) and add `captures:` to them
where a later step needs a value; use step-level `vars:` to pass values, `set:` for derived
ones, `tests:` to add assertions for that step and `replace_tests:` when a step should expect a
different status than the file does. Put anything that needs custom logic in a helper script under
`scripts/` and call it with a `script:` step.

Validate with `hit validate` and then run `hit run <name> --json`. Show me the flow file and a
one-paragraph summary of what each step proved.
```
