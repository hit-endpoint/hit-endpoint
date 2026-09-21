# Prompts for working with hit through an AI assistant

Two ways to use these:

- **Claude Code in this repo**: nothing to paste. The project skill in `.claude/skills/hit/`
  loads automatically when you ask for anything about requests, so just say what you want:
  "add a request to the identity zone that lists Auth0 users", "why does login fail",
  "turn this curl into a request". You can also invoke it explicitly with `/hit <task>`.
- **Any other assistant** (Copilot Chat, Cursor, ChatGPT, a Slack bot): paste the
  [context block](#context-block) once, then one of the task prompts below. Fill in the `<...>`.

Every prompt ends with the same checks: `hit validate`, `hit show`, then `hit run --json`.
That loop is what makes the assistant's YAML trustworthy.

## Context block

Paste this first when the assistant cannot read the repo itself. It is the output of
`hit reference`, so regenerate it after upgrading the tool:

```
hit reference | pbcopy        # macOS
hit reference | clip          # Windows
```

## Task prompts

| File | Use it when |
|---|---|
| [new-request.md](new-request.md) | You have a curl command, an API doc page or a description and want a request file |
| [new-graphql-request.md](new-graphql-request.md) | Same, for a GraphQL operation |
| [write-tests.md](write-tests.md) | A request works but has no or weak tests |
| [new-flow.md](new-flow.md) | Several requests must run in order and pass values along in a chain |
| [setup-server.md](setup-server.md) | A zone has no server yet, or you are onboarding to one |
| [debug-request.md](debug-request.md) | A request fails or returns something unexpected |
| [port-unconverted-script.md](port-unconverted-script.md) | A request file still has an `unconverted:` block of untranslated script |
| [perf-test.md](perf-test.md) | You want a load test and a readable summary of the results |
