# Prompt: debug a failing request

```
The hit request `<ref>` in `zones/<zone>` fails. Here is what I see:

<paste the hit output, or describe the symptom>

Investigate in this order and report what you find at each step before changing anything:
0. `hit sanity`: is the zone ready at all (server, credentials, server reachable)?
1. `hit show <ref>` and `hit show <ref> --curl`: is the rendered URL, auth and body what the
   API expects? Compare with <docs link or a working curl if I have one>.
2. `hit vars --all`: are the variables it depends on set, and from which layer do they come?
3. `hit validate <ref>`: any undefined variables or capture dependencies?
4. `hit run <ref> -v --headers --json`: read the status, response headers and body.

Then propose the fix: a change to the request file, the server, a missing capture from an
earlier request, or a change on my side (credentials, network). Only edit files after telling me
what you will change and why.
```
