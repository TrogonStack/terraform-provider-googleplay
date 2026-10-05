# Resource Lifecycle

This provider has no resources yet (`Resources()` in `provider.go` returns an empty slice). Everything below is illustrative, built around a hypothetical `googleplay_in_app_product` resource (the Google Play Android Developer API's `inappproducts` resource, `packageName`/`sku`-keyed), grounded in the real `*androidpublisher.Service`, `errors.go`, and `client.go`/`retry.go` plumbing that already ships.

## Interface

A resource must implement `resource.Resource`:

```go
type Resource interface {
    Metadata(context.Context, MetadataRequest, *MetadataResponse)
    Schema(context.Context, SchemaRequest, *SchemaResponse)
    Create(context.Context, CreateRequest, *CreateResponse)
    Read(context.Context, ReadRequest, *ReadResponse)
    Update(context.Context, UpdateRequest, *UpdateResponse)
    Delete(context.Context, DeleteRequest, *DeleteResponse)
}
```

Optional interfaces:

- `resource.ResourceWithConfigure`: receive provider client
- `resource.ResourceWithImportState`: support `terraform import`
- `resource.ResourceWithUpgradeState`: handle schema migrations
- `resource.ResourceWithModifyPlan`: resource-level plan modification
- `resource.ResourceWithValidateConfig`: resource-level validation

## Registration

Add a constructor function to the provider's `Resources()` method:

```go
func newInAppProductResource() resource.Resource { return &inAppProductResource{} }

// In provider.go:
func (p *googlePlayProvider) Resources(ctx context.Context) []func() resource.Resource {
    return []func() resource.Resource{
        newInAppProductResource,
    }
}
```

## Metadata

Sets the resource type name as it appears in Terraform configurations:

```go
func (r *inAppProductResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_in_app_product"
}
```

This produces `googleplay_in_app_product` as the resource type (`req.ProviderTypeName` is `"googleplay"`, set in the provider's own `Metadata`).

## Configure

Receive the provider-configured API client:

```go
func (r *inAppProductResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    client, ok := req.ProviderData.(*androidpublisher.Service)
    if !ok {
        resp.Diagnostics.AddError("Unexpected Resource Configure Type",
            fmt.Sprintf("Expected *androidpublisher.Service, got: %T", req.ProviderData))
        return
    }
    r.client = client
}
```

The `nil` check is required: Configure is called during validation when provider data is not yet available.

## Create

Contract:

- Read plan data from `req.Plan`
- Perform the API creation call
- Set ALL attribute values (including computed) in `resp.State`
- Unknown values in plan MUST become known in state (error otherwise)
- On error, the resource is marked tainted for recreation on next plan

```go
func (r *inAppProductResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
    var plan inAppProductResourceModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
    if resp.Diagnostics.HasError() {
        return
    }

    packageName := plan.PackageName.ValueString()
    product := &androidpublisher.InAppProduct{
        PackageName: packageName,
        Sku:         plan.Sku.ValueString(),
    }

    created, err := r.client.Inappproducts.Insert(packageName, product).Context(ctx).Do()
    if err != nil {
        resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to create in-app product: %s", err))
        return
    }

    plan.Status = types.StringValue(created.Status)
    resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

## Read

Contract:

- Read prior state from `req.State`
- Perform the API read call
- If the resource no longer exists: call `resp.State.RemoveResource(ctx)` and return
- Otherwise, update all state values to reflect current API state

A direct get-by-key call, and the real `isNotFound` helper from `errors.go` to detect drift:

```go
func (r *inAppProductResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
    var state inAppProductResourceModel
    resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
    if resp.Diagnostics.HasError() {
        return
    }

    packageName := state.PackageName.ValueString()
    sku := state.Sku.ValueString()

    product, err := r.client.Inappproducts.Get(packageName, sku).Context(ctx).Do()
    if err != nil {
        if isNotFound(err) {
            resp.State.RemoveResource(ctx)
            return
        }
        resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to read in-app product: %s", err))
        return
    }

    state.Status = types.StringValue(product.Status)
    resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

For an endpoint that has no get-by-key (list-only), Read would call the generated `List` method, looping while the response carries a next-page token, and filter client-side by key instead, collapsing "a 404 from the API" and "present in state but absent from every page" into the same `RemoveResource` branch.

## Update

Contract:

- Read plan data from `req.Plan` (the desired new state)
- Perform the API update call
- Set state to reflect the actual post-update values
- All values in state MUST match plan values (or Terraform produces an "inconsistent result" error)

```go
func (r *inAppProductResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
    var plan inAppProductResourceModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
    if resp.Diagnostics.HasError() {
        return
    }

    packageName := plan.PackageName.ValueString()
    sku := plan.Sku.ValueString()
    product := &androidpublisher.InAppProduct{
        PackageName: packageName,
        Sku:         sku,
    }

    updated, err := r.client.Inappproducts.Update(packageName, sku, product).Context(ctx).Do()
    if err != nil {
        resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to update in-app product: %s", err))
        return
    }

    plan.Status = types.StringValue(updated.Status)
    resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
```

`package_name` and `sku` are not expected to change here: both would be marked `RequiresReplace` in the schema (see `references/guides/plan-modification.md`), since the API has no operation to move an in-app product to a different app or rename its SKU after creation.

A resource that instead manages something behind the edits workflow (a store listing, a track, an APK) would not call a single update method here: it would open an edit with `client.Edits.Insert(packageName, &androidpublisher.AppEdit{}).Context(ctx).Do()`, make its change against that edit's ID, and finish with `client.Edits.Commit(packageName, editId).Context(ctx).Do()`.

## Delete

Contract:

- Read prior state from `req.State`
- Perform the API deletion
- If already deleted: return without error (idempotent)
- No need to modify state: framework removes it automatically on success

```go
func (r *inAppProductResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
    var state inAppProductResourceModel
    resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
    if resp.Diagnostics.HasError() {
        return
    }

    packageName := state.PackageName.ValueString()
    sku := state.Sku.ValueString()

    if err := r.client.Inappproducts.Delete(packageName, sku).Context(ctx).Do(); err != nil {
        if isNotFound(err) {
            return
        }
        resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to delete in-app product: %s", err))
        return
    }
}
```

Several Google Play Android Developer API types document no delete operation at all, an edit left uncommitted being the clearest example: the edits overview documents that an open edit can simply be abandoned rather than explicitly deleted. For a resource built on one of those, Delete should return without error and document in the resource's schema description that destroy is a no-op (or whatever the API does support), rather than inventing a delete call that doesn't exist.

## Decoding API Errors

Every non-2xx response from the generated client already comes back as a Go `error` wrapping a `*googleapi.Error{Code int, Message string}` (`google.golang.org/api/googleapi`), not a hand-rolled `Success bool` / `Message string` pair. CRUD methods never need to check a separate success flag, only the returned `error`:

```go
// errors.go, real code:
func isNotFound(err error) bool {
    var apiErr *googleapi.Error
    return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}
```

`errors.As` is required (not a direct type assertion) because an error returned from a CRUD method may be wrapped, e.g. with `fmt.Errorf("...: %w", err)`.

## Related Framework References

| File                                | Contents                                  |
| ------------------------------------ | -------------------------------------------- |
| `framework/resources/index.mdx`     | Resource type definition, full interface  |
| `framework/resources/create.mdx`    | Create method details and caveats         |
| `framework/resources/read.mdx`      | Read method and state refresh             |
| `framework/resources/update.mdx`    | Update method and plan consistency        |
| `framework/resources/delete.mdx`    | Delete method                             |
| `framework/resources/configure.mdx` | Configure method, provider data injection |
