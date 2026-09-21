# Prompt: set up an server and credentials

```
Set up the `<server>` server for `zones/<zone>`.

Servers and public settings: <base URL, tenant names, public client ids, anything non-secret>
Secrets I hold: <list the names only, e.g. client secret, API key; never paste values>
How requests authenticate: <bearer token from a login call | static API key header | basic auth>

Do this:
1. Create `servers/<server>.yaml` with `base_url`, `vars`, and a default `auth:` block.
   Reference every secret as `{{$env:NAME}}` or leave it for the secrets file; never write a value.
2. Create `servers/<server>.secrets.example.yaml` listing the secret variable names with
   placeholder values, so teammates know what to fill in `<env>.secrets.yaml` (gitignored).
3. If auth is a token from a login call, add or point me at the request that captures `token`.
4. Set `default_server` in `zone.yaml` if none is set.
5. Run `hit -z zones/<zone> validate` and tell me which requests still have undefined
   variables and what each one needs.
```
