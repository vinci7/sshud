# Contributing

Thanks for helping. sshud aims to stay small: a light, personal dashboard
for a homelab.

## Build and test

You need Go (see `go.mod`) and Node.js 22. One command builds the UI, runs
`go vet` and the tests, and builds `bin/sshud`:

```sh
make check
```

`./bin/sshud --demo` runs the UI on sample machines; `cd web && npm run dev`
gives hot reload against it.

## Rules

- **New system support comes with samples and tests.** Add the raw probe
  output to `samples/<system>/`, sanitized: no real host names, IP
  addresses, user names or serial numbers. Add parser tests against it.
- **UI uses IBM Carbon only** (`@carbon/react`, `@carbon/charts-react`). No
  other component libraries or custom visual styles.
- **Keep pull requests small**, one change each. Open an issue first for
  anything large, such as a new feature or dependency.
- Security issues go through [SECURITY.md](SECURITY.md), not public issues.
