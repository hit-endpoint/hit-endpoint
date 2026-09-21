# Prompt: port a leftover unconverted script

```
The request file `<path>` in `zones/<zone>` has an `unconverted:` block containing
original test scripts that the importer could not convert. Port it:

- Assertion scripts become entries under `tests:` (see `hit reference` for matchers); keep the
  original test names as `name:`
- Captured server/collection variables become `captures:` (jmespath over `json`, `headers`, `status`)
- Request chaining logic becomes a flow in `flows/`
- Pre-request calls become a separate earlier request with a capture
- Pre-request computations (timestamps, ids) become `vars:` using built-ins like
  `{{$timestampMs}}`, `{{$uuid}}`, or a `hooks: {before: ...}` script if they need custom code

Delete the `unconverted:` block once everything in it is represented. If part of it cannot be
expressed, keep only that part and explain why. Run `hit validate` on the file and show me the
diff. Also remove the corresponding entry from `zones/IMPORT-REPORT.md`.
```
