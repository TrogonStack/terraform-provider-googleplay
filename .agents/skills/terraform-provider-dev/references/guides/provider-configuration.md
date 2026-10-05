# Provider Configuration

## Overview

The provider is the top-level component that:

1. Defines its own configuration schema (auth credentials)
2. Creates the API client during Configure
3. Passes client data to resources and data sources
4. Registers all available resources and data sources

## Provider Interface

```go
type Provider interface {
    Metadata(context.Context, MetadataRequest, *MetadataResponse)
    Schema(context.Context, SchemaRequest, *SchemaResponse)
    Configure(context.Context, ConfigureRequest, *ConfigureResponse)
    Resources(context.Context) []func() resource.Resource
    DataSources(context.Context) []func() datasource.DataSource
}
```

## This Provider's Structure

### Metadata

```go
func (p *googlePlayProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
    resp.TypeName = "googleplay"
    resp.Version = p.version
}
```

`TypeName` becomes the prefix for all resource names (e.g. a hypothetical `googleplay_in_app_product`, since no resource exists yet).

### Schema

Provider schema defines what goes in the `provider "googleplay" {}` block. This part is real, taken directly from `provider.go`:

```go
func (p *googlePlayProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
    resp.Schema = schema.Schema{
        MarkdownDescription: `Manage Google Play with Terraform.

The provider talks to the Google Play Developer API (` + "`androidpublisher`" + `
v3), which automates the tasks otherwise done by hand in the Google Play
Console.

## Authentication

The provider authenticates with Google Cloud using one of, in order:

1. The service account key JSON given in ` + "`credentials`" + ` (or the
   ` + "`GOOGLE_CREDENTIALS`" + ` environment variable).
2. Application Default Credentials (ADC), as described in
   https://cloud.google.com/docs/authentication/application-default-credentials.
   This is what lets the provider run in GitHub Actions under Workload
   Identity Federation without a static key.

Either way, the resulting identity needs access to the Play Console account
whose apps it manages, granted as a user under Users and permissions in the
Play Console, or through a service account linked from there.

Set ` + "`impersonate_service_account`" + ` to have the resolved credentials
impersonate another service account before calling the API, rather than
using those credentials directly. The identity resolved above needs the
Service Account Token Creator role on the impersonated service account.

## Environment variables

| Attribute      | Environment variable |
| -------------- | --------------------- |
| ` + "`credentials`" + ` | ` + "`GOOGLE_CREDENTIALS`" + ` |`,
        Attributes: map[string]schema.Attribute{
            "credentials": schema.StringAttribute{
                Optional:            true,
                Sensitive:           true,
                MarkdownDescription: "The JSON contents of a Google Cloud service account key. When unset, the provider falls back to Application Default Credentials.",
            },
            "impersonate_service_account": schema.StringAttribute{
                Optional:            true,
                MarkdownDescription: "The email address of a service account to impersonate. The credentials resolved from `credentials` or Application Default Credentials must have the Service Account Token Creator role on it.",
            },
        },
    }
}
```

### Configure

Configure creates the API client and makes it available to resources. This is also real, from `provider.go`:

```go
func (p *googlePlayProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
    var data googlePlayProviderModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
    if resp.Diagnostics.HasError() {
        return
    }

    unknown := map[string]bool{
        "credentials":                 data.Credentials.IsUnknown(),
        "impersonate_service_account": data.ImpersonateServiceAccount.IsUnknown(),
    }
    for attribute, isUnknown := range unknown {
        if isUnknown {
            resp.Diagnostics.AddAttributeError(path.Root(attribute), "Unknown Provider Configuration",
                fmt.Sprintf("`%s` depends on a value that is only known after apply, so the provider cannot authenticate during this plan. Use a value known at plan time, or apply the resources it depends on first with -target.", attribute))
        }
    }
    if resp.Diagnostics.HasError() {
        return
    }

    // In tests, skip authentication and use the injected client.
    if testAPIClient != nil {
        resp.DataSourceData = testAPIClient
        resp.ResourceData = testAPIClient
        return
    }

    credentials := data.Credentials.ValueString()
    if credentials == "" {
        credentials = os.Getenv("GOOGLE_CREDENTIALS")
    }

    cfg := clientConfig{
        CredentialsJSON:           credentials,
        ImpersonateServiceAccount: data.ImpersonateServiceAccount.ValueString(),
    }

    service, err := newAndroidPublisherService(ctx, cfg)
    if err != nil {
        resp.Diagnostics.AddError("Configuration Error", "Unable to create the Google Play Android Developer API client: "+err.Error())
        return
    }
    resp.DataSourceData = service
    resp.ResourceData = service
}
```

Unlike a provider that requires a credential and errors when it's missing from both config and environment, this provider treats both attributes as fully optional: an empty `credentials` simply means "fall back to Application Default Credentials" rather than a configuration error. The only configuration errors here are unknown values (an attribute whose value depends on a resource not yet applied) and failures while building the client itself.

### Resource/DataSource Registration

```go
func (p *googlePlayProvider) Resources(ctx context.Context) []func() resource.Resource {
    return []func() resource.Resource{}
}

func (p *googlePlayProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
    return []func() datasource.DataSource{}
}
```

Both are empty today, since this provider has not shipped a resource or data source yet. Adding one means appending its constructor function here (see `references/guides/resource-lifecycle.md` and `references/guides/data-source-lifecycle.md`).

## Client Data Flow

```
provider.Configure()
    -> resp.ResourceData = service
    -> resp.DataSourceData = service

resource.Configure()
    -> req.ProviderData == service (same pointer)
    -> r.client = req.ProviderData.(*androidpublisher.Service)

resource.Create/Read/Update/Delete()
    -> r.client.Inappproducts.Insert(packageName, product).Context(ctx).Do()
    -> r.client.Inappproducts.Get(packageName, sku).Context(ctx).Do()
    -> r.client.Inappproducts.Update(packageName, sku, product).Context(ctx).Do()
    -> r.client.Inappproducts.Delete(packageName, sku).Context(ctx).Do()
```

The last four calls are illustrative (no resource exists yet); `*androidpublisher.Service` itself is real, built in `client.go`:

```go
func newAndroidPublisherService(ctx context.Context, cfg clientConfig) (*androidpublisher.Service, error) {
    tokenSource, err := newTokenSource(ctx, cfg)
    if err != nil {
        return nil, err
    }
    return androidpublisher.NewService(ctx, option.WithHTTPClient(newRetryableClient(tokenSource)))
}
```

It is not a hand-written HTTP client: `androidpublisher.Service` is generated code from `google.golang.org/api/androidpublisher/v3`, maintained by Google, with a typed service per API resource (`client.Inappproducts`, `client.Edits`, `client.Monetization`, and so on). This provider's own code is limited to resolving credentials into an `oauth2.TokenSource` and wrapping that in a retrying `*http.Client`, both passed to `androidpublisher.NewService` via `option.WithHTTPClient`. That option bypasses every other auth-related option the SDK accepts, so the client it is given must already authenticate its own requests, which is exactly what `newRetryableClient` does.

A resource modeling store listings, tracks, or APKs would instead go through the edits workflow: open an edit with `client.Edits.Insert(packageName, &androidpublisher.AppEdit{}).Context(ctx).Do()`, make its change against that edit's ID, then publish with `client.Edits.Commit(packageName, editId).Context(ctx).Do()`. See `AGENTS.md` for what is and is not yet verified about edit lifecycle.

## Environment Variable Fallbacks

Provider config values can fall back to environment variables:

```go
credentials := data.Credentials.ValueString()
if credentials == "" {
    credentials = os.Getenv("GOOGLE_CREDENTIALS")
}
```

This provider supports:

| Attribute                      | Environment variable |
| -------------------------------- | ----------------------- |
| `credentials`                   | `GOOGLE_CREDENTIALS`   |
| `impersonate_service_account`   | (none)                 |

Only `credentials` has an environment fallback; `impersonate_service_account` must be set in the provider block if it's used at all. Neither attribute is required, so there is no "must be set" diagnostic the way a provider with a mandatory credential would have: an empty `credentials` just means Application Default Credentials.

## Authentication

`client.go`'s `newTokenSource` resolves an `oauth2.TokenSource` from `clientConfig`, in order:

```go
const playScope = androidpublisher.AndroidpublisherScope

type clientConfig struct {
    // CredentialsJSON is a service account key's JSON contents. Empty means
    // fall back to Application Default Credentials.
    CredentialsJSON string
    // ImpersonateServiceAccount, when set, is the email address of a service
    // account to impersonate using the credentials resolved above.
    ImpersonateServiceAccount string
}

func newTokenSource(ctx context.Context, cfg clientConfig) (oauth2.TokenSource, error) {
    var baseOpts []option.ClientOption
    if cfg.CredentialsJSON != "" {
        baseOpts = append(baseOpts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(cfg.CredentialsJSON)))
    }

    if cfg.ImpersonateServiceAccount != "" {
        source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
            TargetPrincipal: cfg.ImpersonateServiceAccount,
            Scopes:          []string{playScope},
        }, baseOpts...)
        if err != nil {
            return nil, fmt.Errorf("impersonating %s: %w", cfg.ImpersonateServiceAccount, err)
        }
        return source, nil
    }

    if cfg.CredentialsJSON != "" {
        creds, err := google.CredentialsFromJSONWithTypeAndParams(ctx, []byte(cfg.CredentialsJSON), google.ServiceAccount, google.CredentialsParams{
            Scopes: []string{playScope},
        })
        if err != nil {
            return nil, fmt.Errorf("loading service account credentials: %w", err)
        }
        return creds.TokenSource, nil
    }

    creds, err := google.FindDefaultCredentials(ctx, playScope)
    if err != nil {
        return nil, fmt.Errorf("finding application default credentials: %w", err)
    }
    return creds.TokenSource, nil
}
```

There is no hand-rolled JWT signing anywhere in this provider: the `golang.org/x/oauth2/google` package parses the service account key and produces a token source that signs and refreshes its own tokens. Impersonation wraps whichever base token source was resolved (explicit credentials or ADC) in `impersonate.CredentialsTokenSource`, rather than being a third, independent credential path.

## Retry

`retry.go`'s `newRetryableClient` wraps the resolved `oauth2.TokenSource` in an `oauth2.Transport` (so every attempt, including retries, carries a fresh Authorization header), then wraps that in a `go-retryablehttp` client with a custom `retryPolicy`: retry on connection errors, HTTP 429, and 5xx except 501 (Not Implemented, which retrying cannot fix); POST is never retried, and a cancelled context stops immediately. The provider wires this in unconditionally; there is no provider-level attribute to configure it. Because the Google API Go client's generated `Do()` methods make a single request with no retry logic of their own for ordinary CRUD calls, this transport-level retry does not double up with anything the client already does.

## Test Bypass

Tests inject a client via the package-level `testAPIClient` variable, declared in `provider.go`:

```go
var testAPIClient *androidpublisher.Service

func (p *googlePlayProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
    // ...
    if testAPIClient != nil {
        resp.DataSourceData = testAPIClient
        resp.ResourceData = testAPIClient
        return
    }
    // ... real authentication logic
}
```

`newTestService` in `client_test.go` is what builds a service pointed at a fake: it stands up an `httptest.Server` and builds a real `*androidpublisher.Service` against it with `option.WithEndpoint(server.URL)`, `option.WithHTTPClient(server.Client())`, and `option.WithoutAuthentication()`, so test requests carry no bearer token and the fake server never has to verify one. A future resource test would assign that service to `testAPIClient`, bypassing provider configuration and real credential resolution entirely (see `references/guides/testing.md`).

## Provider Server (main.go)

The entry point wraps the provider into a gRPC server:

```go
package main

import (
    "context"
    "log"

    "github.com/TrogonStack/terraform-provider-googleplay/internal/provider"
    "github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version string = "dev"

func main() {
    err := providerserver.Serve(
        context.Background(),
        provider.New(version),
        providerserver.ServeOpts{
            Address: "registry.terraform.io/trogonstack/googleplay",
        },
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

## Related Framework References

| File                                             | Contents                                    |
| ---------------------------------------------------- | ---------------------------------------------- |
| `framework/providers/index.mdx`                  | Provider interface, metadata, schema        |
| `framework/providers/validate-configuration.mdx` | Provider-level validation                   |
| `framework/provider-servers.mdx`                 | Server setup, protocol versions, debug mode |
| `framework/resources/configure.mdx`              | How resources receive provider data         |
| `framework/data-sources/configure.mdx`           | How data sources receive provider data      |
