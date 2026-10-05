---
name: terraform-provider-dev
description: >
  Use this skill when developing terraform-provider-googleplay: adding
  resources or data sources, designing schemas, implementing CRUD operations,
  plan modification, state upgrades, import, validation, acceptance testing,
  debugging, or any Terraform Plugin Framework work in Go. Also use when the
  user asks about terraform provider patterns, attribute types, or how to
  structure tests. This is the primary development skill for this repository.
---

# Terraform Provider Development (Plugin Framework)

## Mental Model

- Provider = Go server implementing Terraform RPCs (GetProviderSchema, PlanResourceChange, ApplyResourceChange, ReadResource, etc.)
- Resource = struct implementing `resource.Resource` interface: Metadata, Schema, Configure, Create, Read, Update, Delete
- DataSource = struct implementing `datasource.DataSource` interface: Metadata, Schema, Configure, Read
- Schema defines the "shape" of config/plan/state: attributes (leaf values) and blocks (nested structures)
- Plan then Apply: Terraform calls PlanResourceChange (propose changes), then ApplyResourceChange (execute)
- State = Terraform's record of the real world; Plan = expected post-apply state
- Computed attributes: set by the provider from API responses (IDs, timestamps, server-generated values)
- Plugin Framework uses strong Go types: `types.String`, `types.Bool`, `types.Int64`, `types.List`, etc.
- Null vs Unknown: null means the user did not set it; unknown means the value will be known after apply (planned computed)

---

## This Provider: Conventions

This provider ships the provider configuration, authentication and API client today; it has no resources or data sources yet (`Resources()` and `DataSources()` in `provider.go` both return an empty slice). Everything under "Adding a New Resource" and "Adding a Data Source" below is illustrative, built around a hypothetical `googleplay_in_app_product` resource (the Google Play Android Developer API's `inappproducts` resource, `packageName`/`sku`-keyed).

- **Package**: `internal/provider` (single flat package, all resources will live here)
- **File naming**: `resource_<name>.go`, `resource_<name>_test.go`, `data_source_<name>.go`
- **Provider client**: `*androidpublisher.Service` from `google.golang.org/api/androidpublisher/v3`, the official Google-maintained Go client for the Google Play Android Developer API. There is no hand-written HTTP client in this provider; every resource calls a generated service (`client.Edits`, `client.Inappproducts`, `client.Monetization`, and so on)
- **Client injection**: Configure method casts `req.ProviderData.(*androidpublisher.Service)`
- **Auth**: `client.go` resolves a token source from the `credentials` attribute, the `GOOGLE_CREDENTIALS` environment variable, or Application Default Credentials, optionally impersonating a service account on top. `retry.go` wraps that token source in an `oauth2.Transport` plus a retry transport, and the composed `*http.Client` is passed to `androidpublisher.NewService` via `option.WithHTTPClient`
- **ID helper**: no `helpers.go` exists yet, since no resource has been added. When the first resource lands, give it a Computed `id` attribute with `stringplanmodifier.UseStateForUnknown()` if the API has one distinct from its natural key, following the pattern described in `references/guides/schema-design.md`
- **Registration**: new resources and data sources are added to `Resources()` / `DataSources()` in `provider.go`, both currently empty
- **Not-found handling**: the API reports a missing object as an HTTP 404 wrapped in a `*googleapi.Error`. Detect it with `isNotFound(err)` in `errors.go`, which unwraps with `errors.As` (a wrapped error would otherwise fail a direct type check)
  - **Read**: call `resp.State.RemoveResource(ctx)` and return (resource was deleted externally)
  - **Delete**: return without error where the API has no delete and documents a no-op instead
- **Edits workflow**: most changes to an app's public content (store listings, tracks, APKs) go through `edits.insert` / mutate / `edits.commit`, not a direct update call. A resource that models one of these needs to open an edit, make its change, and commit in `Create`/`Update`, and should not assume the simpler "one call per write" shape that `inappproducts` has. See `AGENTS.md` for what is and is not verified about edit lifecycle
- **Source of truth**: model every resource on Google's [Android Developer API reference](https://developers.google.com/android-publisher/api-ref/rest) and the `androidpublisher` package generated from it. Never add fields or behaviors the API does not describe
- **Retry**: `retry.go` wraps the client's HTTP transport with automatic retry on 429 and 5xx except 501, honoring `Retry-After`, and never retries POST. No configuration attribute
- **Testing**: no resource tests exist yet. `helpers_test.go` builds a well-formed but fake service account key (`newTestServiceAccountJSON`); `client_test.go` points a real `*androidpublisher.Service` at an `httptest.Server` with `option.WithEndpoint(server.URL)`, `option.WithHTTPClient(server.Client())`, and `option.WithoutAuthentication()` (see `newTestService`). A future resource's tests would inject that service as the package-level `testAPIClient`, bypassing provider configuration, backed by an in-memory `httptest` fake of the endpoints it calls. `TestLive_*` in `live_test.go` runs against a real Google Play Console account via Application Default Credentials, skipped unless `TF_ACC` and `GOOGLEPLAY_TEST_PACKAGE_NAME` are set

---

## Adding a New Resource

1. Create `internal/provider/resource_<name>.go`
2. Define model struct(s) with `tfsdk` tags
3. Implement the resource:

```go
var (
    _ resource.Resource                = &inAppProductResource{}
    _ resource.ResourceWithImportState = &inAppProductResource{}
)

func newInAppProductResource() resource.Resource { return &inAppProductResource{} }

type inAppProductResource struct {
    client *androidpublisher.Service
}

type inAppProductResourceModel struct {
    PackageName types.String `tfsdk:"package_name"`
    Sku         types.String `tfsdk:"sku"`
    Status      types.String `tfsdk:"status"`
}

func (r *inAppProductResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_in_app_product"
}

func (r *inAppProductResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "package_name": schema.StringAttribute{
                Required:      true,
                PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
            },
            "sku": schema.StringAttribute{
                Required:      true,
                PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
            },
            "status": schema.StringAttribute{Computed: true},
        },
    }
}

func (r *inAppProductResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    client, ok := req.ProviderData.(*androidpublisher.Service)
    if !ok {
        resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *androidpublisher.Service, got: %T", req.ProviderData))
        return
    }
    r.client = client
}
```

4. Implement Create, Read, Update, Delete (see guide: `references/guides/resource-lifecycle.md`)
5. Implement ImportState
6. Register in `provider.go`: add `newInAppProductResource` to `Resources()` return slice
7. Create `internal/provider/resource_in_app_product_test.go` (see guide: `references/guides/testing.md`)

---

## Adding a Data Source

The provider does not define any data sources yet (`DataSources()` returns an empty slice). The shape below is illustrative, built around the same hypothetical `googleplay_in_app_product` resource, reading it back with `inappproducts.get`:

```go
var _ datasource.DataSource = &inAppProductDataSource{}

func newInAppProductDataSource() datasource.DataSource { return &inAppProductDataSource{} }

type inAppProductDataSource struct {
    client *androidpublisher.Service
}

type inAppProductDataSourceModel struct {
    PackageName types.String `tfsdk:"package_name"`
    Sku         types.String `tfsdk:"sku"`
    Status      types.String `tfsdk:"status"`
}

func (d *inAppProductDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_in_app_product"
}

func (d *inAppProductDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "package_name": schema.StringAttribute{Required: true},
            "sku":          schema.StringAttribute{Required: true},
            "status":       schema.StringAttribute{Computed: true},
        },
    }
}

func (d *inAppProductDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    client, ok := req.ProviderData.(*androidpublisher.Service)
    if !ok {
        resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *androidpublisher.Service, got: %T", req.ProviderData))
        return
    }
    d.client = client
}

func (d *inAppProductDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
    var data inAppProductDataSourceModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
    if resp.Diagnostics.HasError() {
        return
    }

    product, err := d.client.Inappproducts.Get(data.PackageName.ValueString(), data.Sku.ValueString()).Context(ctx).Do()
    if err != nil {
        resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to read in-app product: %s", err))
        return
    }

    data.Status = types.StringValue(product.Status)
    resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
```

Register: add `newInAppProductDataSource` to `DataSources()` in `provider.go`.

---

## Schema Design Quick-Reference

| Schema Type                                                                                    | Go Model Type            | When to Use                    |
| ---------------------------------------------------------------------------------------------- | ------------------------ | ------------------------------- |
| `schema.StringAttribute{Required: true}`                                                       | `types.String`           | User must provide              |
| `schema.StringAttribute{Optional: true}`                                                       | `types.String`           | User may provide               |
| `schema.StringAttribute{Computed: true}`                                                       | `types.String`           | Server-generated only          |
| `schema.StringAttribute{Optional: true, Computed: true}`                                       | `types.String`           | User provides OR server fills  |
| `schema.StringAttribute{Sensitive: true}`                                                      | `types.String`           | Credential value (provider's `credentials`) |

The table above reflects this provider's real `credentials` attribute; the rest is illustrative until a resource exists. A hypothetical `googleplay_in_app_product` would mark `package_name` and `sku` Required (the API has no rename or re-parent operation for either) and `status` Computed, since it reflects whether the product is active in the store rather than something the caller sets directly.

### Plan Modifiers

| Modifier                                  | Use Case                                   |
| ------------------------------------------ | ------------------------------------------ |
| `stringplanmodifier.UseStateForUnknown()` | Computed value stable across updates (an `id` attribute) |
| `stringplanmodifier.RequiresReplace()`    | Changing this forces resource recreation (immutable fields, e.g. `package_name` or `sku` on `googleplay_in_app_product`) |

Full details: `references/guides/schema-design.md`

---

## Testing Patterns

### Test Infrastructure

No acceptance-test harness (`resource.Test`, a provider factory map) exists yet, since there are no resources to exercise that way. What exists today are direct unit tests of the provider and client:

```go
func configureProvider(t *testing.T, config map[string]string) *provider.ConfigureResponse {
    t.Helper()
    ctx := context.Background()
    p := New("test")()

    schemaResp := &provider.SchemaResponse{}
    p.Schema(ctx, provider.SchemaRequest{}, schemaResp)
    // build a tftypes.Value from config and call p.Configure(...)
}
```

`TestProviderConfigure_FromAttributes`, `TestProviderConfigure_FromEnvironment`, `TestProviderConfigure_Errors`, and `TestProviderConfigure_UsesTestClient` in `provider_test.go` exercise this directly, without ever building a Terraform config string.

### Test Structure (what a future resource test will look like)

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

This is the real `newTestService` in `client_test.go`. A resource test would inject the resulting service as `testAPIClient` (bypassing provider `Configure` entirely) and drive it through `resource.Test` once the first resource exists.

### Running Tests

```bash
go test ./internal/provider/ -v -run TestClient
go test ./internal/provider/ -v -run TestProviderConfigure
mise run test:live   # TestLive_* against a real Google Play Console account
```

Full details: `references/guides/testing.md`

---

## State Upgrade

No resource exists yet, so none has needed a state upgrade. If a future resource's schema needs a breaking change after it ships (e.g. splitting a flat attribute into a nested object):

1. Increment `Version` in the schema
2. Implement `resource.ResourceWithUpgradeState`
3. Parse raw JSON state and write to current model

```go
func (r *inAppProductResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
    return map[int64]resource.StateUpgrader{
        0: {
            StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
                var raw map[string]json.RawMessage
                if err := json.Unmarshal(req.RawState.JSON, &raw); err != nil {
                    resp.Diagnostics.AddError("State Upgrade Error", fmt.Sprintf("Unable to parse raw state: %s", err))
                    return
                }
                // Parse old format, build new model, set state
                resp.Diagnostics.Append(resp.State.Set(ctx, &newModel)...)
            },
        },
    }
}
```

Full details: `references/guides/state-management.md`

---

## Reference Docs

### Topic Guides (synthesized, task-oriented)

| Guide                                         | Contents                                              |
| ---------------------------------------------- | ------------------------------------------------------ |
| `references/guides/resource-lifecycle.md`     | CRUD methods, interface contracts, registration       |
| `references/guides/data-source-lifecycle.md`  | Data source pattern, Read method                      |
| `references/guides/schema-design.md`          | Attributes, blocks, types, nested models              |
| `references/guides/plan-modification.md`      | UseStateForUnknown, RequiresReplace, custom modifiers |
| `references/guides/state-management.md`       | Import, state upgrade, private state                  |
| `references/guides/validation.md`             | Attribute validators, resource-level validation       |
| `references/guides/testing.md`                | Unit tests, httptest fakes, live acceptance tests     |
| `references/guides/provider-configuration.md` | Provider setup, client injection, auth                |
| `references/guides/functions.md`              | Provider-defined functions (Terraform 1.8+)           |

### Framework Reference (verbatim, upstream HashiCorp docs)

Key entry points in `references/framework/`:

| File                                 | Contents                         |
| ------------------------------------- | --------------------------------- |
| `resources/index.mdx`                | Resource interface, registration |
| `resources/create.mdx`               | Create method contract           |
| `resources/read.mdx`                 | Read method, refresh state       |
| `resources/update.mdx`               | Update method, in-place changes  |
| `resources/delete.mdx`               | Delete method                    |
| `resources/configure.mdx`            | Client injection into resources  |
| `resources/import.mdx`               | Import state support             |
| `resources/plan-modification.mdx`    | Plan modifiers                   |
| `resources/state-upgrade.mdx`        | State upgrade for schema changes |
| `data-sources/index.mdx`             | Data source interface            |
| `handling-data/schemas.mdx`          | Schema definition                |
| `handling-data/accessing-values.mdx` | Reading config/plan/state        |
| `handling-data/writing-state.mdx`    | Writing to response state        |
| `handling-data/attributes/index.mdx` | All attribute types              |
| `handling-data/blocks/index.mdx`     | All block types                  |
| `handling-data/types/index.mdx`      | Type system (Go value types)     |
| `validation.mdx`                     | Validation patterns              |
| `diagnostics.mdx`                    | Error/warning diagnostics        |
| `acctests.mdx`                       | Acceptance testing setup         |
| `debugging.mdx`                      | Debugging providers              |
| `providers/index.mdx`                | Provider interface               |
| `provider-servers.mdx`               | Provider server (main.go)        |
| `functions/implementation.mdx`       | Provider functions               |
| `migrating/index.mdx`                | SDKv2 migration overview         |
