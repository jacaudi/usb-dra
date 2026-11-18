# Development

This guide covers running the checks and what the Docker build does.

The Docker build does not run tests. Run `task test` locally or in CI; it needs
Helm on the `PATH`, because the chart tests render the chart. `.dockerignore`
strips `testdata` and `*_test.go` from the build context, so the `go test` step
in the `Dockerfile` finds nothing to run.

```sh
task test                       # go test ./... -race
task ci                         # everything CI runs, helm lint + template included
task artifact VERSION=v0.1.0    # renders install.yaml from examples/values.yaml
```

[← Back to README](../README.md)
