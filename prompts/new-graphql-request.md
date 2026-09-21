# Prompt: create a GraphQL request file

```
Create a hit request file for this GraphQL operation in `zones/<zone>/collections/<collection>`:

<paste the query or mutation, and example variables>

Use `body: {graphql: {query: ..., variables: {...}}}` with `{{...}}` placeholders for the
variable values that change per run, and set the operation's default values under `vars:`.
Follow the collection's `_defaults.yaml` for URL and headers. Tests must include
`status: 200` and `json: {errors: {exists: false}}` plus one check on the data path that
matters, for example `"data.<field>.id": {exists: true}`. Add `captures:` for returned ids.

Run `hit -z zones/<zone> validate <collection>` and `hit show <ref>`, then show me
the YAML.
```
