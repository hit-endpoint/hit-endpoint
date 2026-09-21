# Prompt: create a request file

```
Create a hit request file in zone `zones/<zone>`, collection `<collection>`.

Source: <paste a curl command, the relevant API doc excerpt, or describe the call>

Before writing:
- run `hit -z zones/<zone> ls <collection> --json` and read that collection's
  `_defaults.yaml` plus one neighbouring request, and reuse their base URL variable, header and
  auth conventions instead of repeating them in the new file
- pick the next numeric prefix in the folder and a file name like NN-verb-noun.yaml

The file must:
- use `{{...}}` variables for anything server-specific (host, ids, credentials); never a literal secret
- use `body: {json: ...}` for JSON payloads, `query:` for query parameters
- include tests: the expected status, and one or two checks on the response shape
- add `captures:` for any id or token later requests will need

Then run `hit -z zones/<zone> validate <collection>` and `hit show <new ref> --curl`,
fix any problems, and show me the final YAML and the rendered curl. Do not send the request
unless I say so.
```
