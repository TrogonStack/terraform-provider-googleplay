# terraform-provider-googleplay

Terraform provider for the Google Play Developer API. Resources and data sources are added one at a time as they are needed.

- **Module**: `github.com/TrogonStack/terraform-provider-googleplay`
- **Package**: `internal/provider/` (single flat package, all resources here)

## Commands

```bash
mise run test          # go test -count=1 -cover -skip TestLive ./...
mise run test:live     # TestLive_* against a real Google Play Console account
mise run lint          # golangci-lint run --fix ./...
mise run build         # full CI pipeline (download, tidy, lint, test, docs, diff)
mise run docs          # regenerate docs/ from schema descriptions
```

Single test:

```bash
go test ./internal/provider/ -v -run TestClient
```

Runtime credentials the provider itself needs (not required to run the unit tests, which never call a real API):

- `GOOGLE_CREDENTIALS`: optional, the JSON contents of a Google Cloud service account key. When unset, the provider falls back to Application Default Credentials (ADC), which is what lets it authenticate in CI under Workload Identity Federation without a static key.

## Skills

Always load `terraform-provider-dev` when working on a resource or data source.

## Architecture

- File naming: `resource_<name>.go`, `resource_<name>_test.go`, `data_source_<name>.go`
- Provider client: `*androidpublisher.Service` from `google.golang.org/api/androidpublisher/v3`, the official Google-maintained client for this API
- Auth: `client.go` resolves a token source from explicit credentials, ADC, or either impersonating a service account on top of them, then `retry.go` wraps it in an `oauth2.Transport` plus a retry transport before passing the composed `*http.Client` to `androidpublisher.NewService` via `option.WithHTTPClient`. That option bypasses every other auth-related option the SDK accepts, so the client it is given must already authenticate its own requests
- New resources must be registered in `provider.go` `Resources()` / `DataSources()`

## Conventions

### Source of truth

Model every resource on Google's [Android Developer API reference](https://developers.google.com/android-publisher/api-ref/rest) and the `androidpublisher` package it is generated from. Never add fields or behaviors the API does not describe; when it is ambiguous, say so in the resource description.

### Resource naming

Use the same terminology as the API. A resource for an in-app product is `googleplay_in_app_product`, and attribute names are the snake_case form of the API's field names.

### Not-found handling

Detect a missing object with the shared `isNotFound(err)` helper in `errors.go`, which unwraps a `*googleapi.Error` with `errors.As` and checks for HTTP 404.

- **Read**: call `resp.State.RemoveResource(ctx)` and return (resource was deleted externally)
- **Delete**: return without error where the API has no delete and documents a no-op instead (see "Edits" below)

### Retry

Automatic retry on 429 and 5xx except 501, honoring `Retry-After`, with a 90 second timeout per attempt. POST is never retried, since the API can fail a create after it has already taken effect. No configuration attribute: the transport in `retry.go` is fixed. The generated client's `Do()` methods make a single request with no retry of their own (confirmed by reading `androidpublisher`'s generated call code down to `gensupport.SendRequest`; the package's own retry logic is wired up only for resumable media uploads), so this layer does not double-retry.

### Context propagation

Every generated call accepts `.Context(ctx)`, which Terraform's CRUD methods should set from their own `ctx` so cancellation reaches in-flight requests.

## Google Play API facts that shape future resources

These are the facts about the API's own behavior known so far, verified against Google's documentation at the time they were written. Re-verify before relying on them, since a maintenance-mode API can still change its documented behavior.

- **Apps cannot be created through this API.** Every endpoint in the API reference (`edits`, `inappproducts`, `monetization`, `purchases`, and so on) takes an existing `packageName` as a path parameter; none creates one. A `googleplay_app` resource is therefore not possible as a create/destroy resource; at most it could be a data source, or a resource that only imports and manages settings on an app created by hand in the Play Console.
- **Most changes to an app's public content go through an edit.** `edits.insert` opens an edit, seeded from the app's current live state; individual changes (store listings, APKs, tracks, and so on) are made against that edit's ID; `edits.commit` publishes every change in the edit atomically, after validation. `edits.get`, `edits.insert`, and `edits.commit` are documented behavior; this provider has not yet implemented any resource that uses them.
- **An edit does not have to be explicitly deleted to discard it.** The edits overview documents `edits.delete` as a way to abandon an edit, but also says the edit can simply be left alone: "You can also abandon the edit at any time, discarding the changes and leaving your app as it was." It also documents that opening a new edit invalidates whatever edit a user already had open. Edit expiration and concurrent-edit behavior beyond that are not covered in the pages read so far and should be verified before a resource depends on them.
- **Users are managed at the developer account level, but their grants are not uniformly account-wide.** The account's users (`accounts.users`) is the console's "Users and permissions" list, not a per-app resource. Within that, a user's permissions documented in the API combine account-wide permissions with a list of per-app permission grants, so "grant" is not simply "account or nothing": a future resource modeling a grant needs to represent both scopes, not assume one.

## CI

- PR: lint + test + build (GitHub Actions)
- Release: release-please + goreleaser on push to main
