# Contributing

## Prerequisites

- [mise](https://mise.jdx.dev), which pins the Go, `golangci-lint`, GoReleaser, and `tfplugindocs` versions used by CI

Install the toolchain with `mise install`. Every command below runs through `mise` so local runs match CI.

## Development

```bash
mise run build      # full CI pipeline: download, lint, test, tidy, docs, diff
mise run test       # go test -count=1 -cover -skip TestLive ./...
mise run test:live  # live tests against a real Google Play Console account
mise run lint       # golangci-lint run --fix ./...
mise run docs       # regenerate docs/ from schema descriptions
```

`mise run build` is what CI runs on every pull request, including the `git diff --exit-code` check, so run it before pushing.

## Testing

Unit tests never reach Google Play. They serve canned JSON responses from an `httptest.Server` and point a real `*androidpublisher.Service` at it with `option.WithEndpoint(server.URL)`, `option.WithHTTPClient(server.Client())`, and `option.WithoutAuthentication()`. Resource tests should inject that service as `testAPIClient`, which bypasses provider configuration entirely.

```bash
mise exec -- go test ./internal/provider/ -v -run TestClient
```

Live tests are named `TestLive_*` and skip unless `TF_ACC` and `GOOGLEPLAY_TEST_PACKAGE_NAME` are set. They authenticate with Application Default Credentials, the same way the provider does in CI, so no credentials of their own are required. Run them with `mise run test:live` against an app you can safely modify.

## Code layout

All code lives in the flat `internal/provider/` package. Resources go in `resource_<name>.go` and data sources in `data_source_<name>.go`, with tests alongside as `<file>_test.go`. New resources must be registered in the `Resources()` or `DataSources()` method in `provider.go`, or the provider will not expose them.

Model every request and response on Google's [Android Developer API reference](https://developers.google.com/android-publisher/api-ref/rest) and the `google.golang.org/api/androidpublisher/v3` package it is generated from. Do not add fields or behaviors the API does not describe.

On a missing object, use the shared `isNotFound(err)` helper, which unwraps with `errors.As` into `*googleapi.Error`. `Read` should call `resp.State.RemoveResource(ctx)` and return; `Delete` should return without an error.

## Commits

Commits follow [Conventional Commits](https://www.conventionalcommits.org) and require a [DCO](https://developercertificate.org) sign-off:

```bash
git commit -s -m "fix(client): retry 503 responses on idempotent methods only"
```

The commit type determines the next version, so it is worth getting right.

## Releases

[release-please](https://github.com/googleapis/release-please) reads the conventional commits merged into `main` and maintains an open release pull request with the computed version bump and changelog entries. Merging that pull request tags the release and publishes the provider archives, plus a GPG-signed checksum file, via [GoReleaser](https://goreleaser.com). No release happens without that pull request being merged.

Each release carries the assets the provider registry protocol expects: one zip per platform, a `SHA256SUMS` file, a detached GPG signature over it, and `terraform-provider-googleplay_<version>_manifest.json` built from `terraform-registry-manifest.json` at the repository root. That manifest declares plugin protocol 6, which `providerserver.Serve` uses because `main.go` leaves `ProtocolVersion` unset. Registries assume protocol 5.0 when the manifest is missing, so a release without it installs and then fails to load.
