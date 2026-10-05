# Testing

## Overview

This provider's tests never reach the real Google Play Developer API, except the `TestLive_*` tests gated behind `TF_ACC`. There are three layers today:

1. **Provider configuration tests** (`provider_test.go`): exercise `googlePlayProvider.Configure` directly, without building a Terraform config string or running `resource.Test`
2. **Client tests** (`client_test.go`): build a real `*androidpublisher.Service` against an `httptest.Server`, verifying credential resolution and not-found error decoding
3. **Retry policy tests** (`retry_test.go`): unit tests of the `retryPolicy` function plus `newRetryableClient`'s end-to-end retry behavior over a real HTTP server

No resource or data source exists yet, so there is no `resource.Test`-based acceptance test harness. The final section below describes the pattern a future resource's tests would follow.

## Test Infrastructure

`helpers_test.go` has the shared building blocks:

```go
func newTestServiceAccountJSON(t *testing.T) string {
    // Generates a fresh RSA key, PEM-encodes it, and marshals a well-formed
    // (but fake) service account key JSON document around it: type, project_id,
    // private_key_id, private_key, client_email, client_id, token_uri.
}

func writeJSON(w http.ResponseWriter, status int, body any) {
    // Writes a JSON body with a status code in test HTTP handlers.
}
```

`newTestServiceAccountJSON` never needs to produce a signature anyone verifies: it parses successfully as a service account key and never reaches the network until something calls `Token()` on the resulting token source, which none of these tests do (tests that build a service against a fake server pass `option.WithoutAuthentication()` instead).

## Provider Configuration Tests

`provider_test.go` builds a provider, drives `Configure` with a hand-built `tftypes.Value`, and asserts on the response, entirely without Terraform's test driver:

```go
func configureProvider(t *testing.T, config map[string]string) *provider.ConfigureResponse {
    t.Helper()
    ctx := context.Background()
    p := New("test")()

    schemaResp := &provider.SchemaResponse{}
    p.Schema(ctx, provider.SchemaRequest{}, schemaResp)

    values := map[string]tftypes.Value{}
    for _, name := range []string{"credentials", "impersonate_service_account"} {
        if v, ok := config[name]; ok {
            values[name] = tftypes.NewValue(tftypes.String, v)
        } else {
            values[name] = tftypes.NewValue(tftypes.String, nil)
        }
    }
    raw := tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), values)

    resp := &provider.ConfigureResponse{}
    p.Configure(ctx, provider.ConfigureRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: raw}}, resp)
    return resp
}

func clearCredentialEnv(t *testing.T) {
    t.Setenv("GOOGLE_CREDENTIALS", "")
}
```

Tests built on this:

- `TestProviderConfigure_FromAttributes`: `credentials` set in config, asserts `resp.ResourceData` is an `*androidpublisher.Service` shared with `resp.DataSourceData`
- `TestProviderConfigure_FromEnvironment`: same, but via the `GOOGLE_CREDENTIALS` environment variable and no config
- `TestProviderConfigure_Errors`: invalid `credentials` (not valid JSON), asserting the diagnostic detail contains `"loading service account credentials"`
- `TestProviderConfigure_UsesTestClient`: sets the package-level `testAPIClient` and asserts Configure returns it unchanged, bypassing credential resolution entirely
- `TestProviderConfigure_UnknownValues`: table-driven over both provider attributes, asserting an unknown value (one that depends on a resource not yet applied) produces an attribute-scoped error mentioning "only known after apply", and that no client is built while it's unresolved

## Client Tests

`client_test.go`'s `newTestService` is the shared fixture: it stands up an `httptest.Server` and builds a real `*androidpublisher.Service` against it, bypassing authentication entirely rather than verifying a bearer token:

```go
func newTestService(t *testing.T, handler http.HandlerFunc) *androidpublisher.Service {
    t.Helper()
    server := httptest.NewServer(handler)
    t.Cleanup(server.Close)

    service, err := androidpublisher.NewService(t.Context(),
        option.WithEndpoint(server.URL),
        option.WithHTTPClient(server.Client()),
        option.WithoutAuthentication(),
    )
    if err != nil {
        t.Fatalf("failed to build a test service: %v", err)
    }
    return service
}
```

Each test calls `newTestService` with its own handler and gets back a ready-to-use `*androidpublisher.Service`:

- `TestNewTokenSource_FromCredentialsJSON`, `TestNewTokenSource_InvalidCredentialsJSON`, `TestNewTokenSource_ApplicationDefaultCredentialsError`, `TestNewTokenSource_Impersonation`: exercise `newTokenSource`'s four branches (explicit service account JSON, an invalid document, falling back to Application Default Credentials with `GOOGLE_APPLICATION_CREDENTIALS` pointed at a file that doesn't exist, and impersonation layered on top of explicit credentials) without ever reaching the network, since building a token source does not fetch a token
- `TestNewAndroidPublisherService_BuildsClient`: asserts `newAndroidPublisherService` returns a usable service (checking `service.Edits != nil` as a proxy for "the generated service initialized correctly")
- `TestIsNotFound`: against a handler returning a 404 `googleapi`-shaped error body, asserts `isNotFound(err)` is true, the error message mentions the status code, and that a wrapped `*googleapi.Error` is still detected by `isNotFound` while a plain, unrelated error is not
- `TestIsNotFound_OtherStatusCodes`: a 403 response, asserting `isNotFound` returns false for a status code that isn't 404

Because tests authenticate with `option.WithoutAuthentication()` rather than a real credential, there is no token or signature for the fake server to check, unlike a hand-rolled bearer-token scheme would require.

## Retry Policy Tests

`retry_test.go` has two kinds of tests. The policy function itself, `retryPolicy`, is tested directly with no server involved, since it's a plain `func(context.Context, *http.Response, error) (bool, error)`:

```go
func TestRetryPolicy_429_Retries(t *testing.T) {
    resp := &http.Response{StatusCode: 429}
    retry, err := retryPolicy(context.Background(), resp, nil)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !retry {
        t.Error("expected retry on 429")
    }
}
```

The full table: `_429_Retries`, `_500_Retries`, `_502_Retries`, `_501_DoesNotRetry` (501 is not transient), `_200_DoesNotRetry`, `_404_DoesNotRetry`, `_ConnectionError_Retries` (a non-nil `err` with no response), `_CancelledContext_DoesNotRetry`, `_PostNeverRetries` (table-driven over a server error, a 429, and a transport error, all with the request method tagged as POST via context).

The second kind drives `newRetryableClient` end-to-end against a real `httptest.Server`, since the actual retrying happens in the composed `*http.Client`, not in `retryPolicy` alone: `TestRetryableClient_BoundsEachAttempt` (asserts the per-attempt timeout), `TestRetryableClient_DoesNotRetryPost`, `TestRetryableClient_RetriesIdempotentMethods` (GET, PATCH, DELETE), `TestRetryableClient_ExhaustsAfterFiveRetries`.

## Live Acceptance Tests

`live_test.go` runs against a real Google Play Console account, skipped unless both `TF_ACC` and `GOOGLEPLAY_TEST_PACKAGE_NAME` are set:

```go
func requireLivePackageName(t *testing.T) string {
    t.Helper()
    if os.Getenv("TF_ACC") == "" {
        t.Skip("set TF_ACC=1 to run live acceptance tests against a real Google Play Console account")
    }
    packageName := os.Getenv("GOOGLEPLAY_TEST_PACKAGE_NAME")
    if packageName == "" {
        t.Skip("set GOOGLEPLAY_TEST_PACKAGE_NAME to run live acceptance tests against a real Google Play Console account")
    }
    return packageName
}

// TestLive_Authenticates exercises Application Default Credentials against
// the real API: insert an edit, then delete it, leaving nothing behind.
// Credentials come entirely from the environment (ADC), the same way the
// provider authenticates in GitHub Actions under Workload Identity
// Federation, so this test takes no credentials of its own.
func TestLive_Authenticates(t *testing.T) {
    packageName := requireLivePackageName(t)

    service, err := newAndroidPublisherService(t.Context(), clientConfig{})
    if err != nil {
        t.Fatalf("failed to build a live client: %v", err)
    }

    edit, err := service.Edits.Insert(packageName, &androidpublisher.AppEdit{}).Do()
    if err != nil {
        t.Fatalf("expected an authenticated request to succeed, got: %v", err)
    }
    t.Cleanup(func() {
        if err := service.Edits.Delete(packageName, edit.Id).Do(); err != nil {
            t.Logf("failed to delete the edit created by this test: %v", err)
        }
    })
}
```

Unlike the App Store Connect style of this guide's upstream counterpart, this test takes no credentials of its own: it relies entirely on Application Default Credentials being available in the environment it runs in. `mise run test` runs `go test -count=1 -cover -skip TestLive ./...`, so these never run by accident; `mise run test:live` runs them explicitly.

## Testing a Future Resource

No resource exists yet, so this is illustrative, following the same `httptest` style already used in `client_test.go` rather than a hand-rolled fake service interface. For a hypothetical `googleplay_in_app_product`:

1. Stand up an `httptest.Server` with handlers for the `inappproducts` paths it calls (insert, get, update, delete, keyed by `packageName`/`sku`), backed by an in-memory map
2. Build a service against it with `androidpublisher.NewService(ctx, option.WithEndpoint(server.URL), option.WithHTTPClient(server.Client()), option.WithoutAuthentication())` (the real `newTestService` helper) and assign it to the package-level `testAPIClient`, bypassing provider `Configure`
3. Add a provider factory map and minimal provider config block, mirroring other Terraform Plugin Framework providers:

```go
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
    "googleplay": providerserver.NewProtocol6WithError(New("test")()),
}

const testProviderConfig = `
provider "googleplay" {}
`
```

4. Drive `resource.Test` the usual way:

```go
func TestAccInAppProduct_Basic(t *testing.T) {
    // set up the httptest.Server and testAPIClient as described above

    resource.Test(t, resource.TestCase{
        ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
        Steps: []resource.TestStep{
            {
                Config: testProviderConfig + `
resource "googleplay_in_app_product" "test" {
  package_name = "com.example.app"
  sku          = "premium_upgrade"
}
`,
                Check: resource.ComposeAggregateTestCheckFunc(
                    resource.TestCheckResourceAttr("googleplay_in_app_product.test", "package_name", "com.example.app"),
                    resource.TestCheckResourceAttrSet("googleplay_in_app_product.test", "status"),
                ),
            },
        },
    })
}
```

5. For a not-found/drift test, delete the item from the in-memory map between steps and use `PlanOnly` + `ExpectNonEmptyPlan`, the same way `client_test.go`'s fixtures are reset between cases
6. For import, add an `ImportState: true, ImportStateVerify: true` step; if a field cannot be reconstructed from a read (write-only, or not returned by the API), add it to `ImportStateVerifyIgnore`

## Check Functions

| Function                                                       | Use Case                     |
| ------------------------------------------------------------------ | --------------------------------- |
| `resource.TestCheckResourceAttr(name, key, value)`             | Exact attribute match        |
| `resource.TestCheckResourceAttrSet(name, key)`                 | Attribute is set (any value) |
| `resource.TestCheckNoResourceAttr(name, key)`                  | Attribute is NOT set         |
| `resource.TestCheckTypeSetElemAttr(name, key, value)`          | Element present in a Set attribute |
| `resource.TestCheckResourceAttrPair(name1, key1, name2, key2)` | Two attributes match         |
| `resource.ComposeAggregateTestCheckFunc(...)`                  | Combine multiple checks      |

## Running Tests

```bash
go test ./internal/provider/ -v -run TestClient
go test ./internal/provider/ -v -run TestProviderConfigure
go test ./internal/provider/ -v -run TestRetryPolicy

# Full suite, matching `mise run test` (skips TestLive_*)
go test -count=1 -cover -skip TestLive ./...

# Live acceptance tests against a real Google Play Console account
mise run test:live
```

## Test Naming Convention

```
Test<Subject>_<Scenario>
```

Examples from this provider: `TestNewTokenSource_FromCredentialsJSON`, `TestNewTokenSource_Impersonation`, `TestNewAndroidPublisherService_BuildsClient`, `TestIsNotFound`, `TestIsNotFound_OtherStatusCodes`, `TestProviderConfigure_FromAttributes`, `TestProviderConfigure_FromEnvironment`, `TestProviderConfigure_Errors`, `TestProviderConfigure_UsesTestClient`, `TestProviderConfigure_UnknownValues`, `TestRetryPolicy_429_Retries`, `TestRetryableClient_RetriesIdempotentMethods`, `TestLive_Authenticates`. A future resource's acceptance tests would follow `TestAcc<Resource>_<Scenario>` (e.g. `TestAccInAppProduct_Basic`), matching the Plugin Framework's own convention.

## Related Framework References

| File                      | Contents                             |
| --------------------------- | ----------------------------------------- |
| `framework/acctests.mdx`  | Acceptance test setup with framework |
| `framework/debugging.mdx` | Debugging test failures              |
