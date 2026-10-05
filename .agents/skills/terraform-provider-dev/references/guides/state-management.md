# State Management

## Import

Import lets practitioners bring existing resources under Terraform management without recreating them. This provider has no resources yet, so nothing implements `resource.ResourceWithImportState` today. The shapes below are illustrative.

### Compound Import (Custom Parsing)

A hypothetical `googleplay_in_app_product` has no single opaque ID of its own: the API keys it by `package_name` plus `sku` together, so import needs both values, parsed by hand from a compound ID with a small helper type and function:

```go
type inAppProductImportID struct {
    PackageName string
    Sku         string
}

func parseInAppProductImportID(raw string) (inAppProductImportID, error) {
    parts := strings.SplitN(raw, "/", 2)
    if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
        return inAppProductImportID{}, fmt.Errorf("expected import ID in the format <package_name>/<sku>, got: %q", raw)
    }
    return inAppProductImportID{PackageName: parts[0], Sku: parts[1]}, nil
}

func (r *inAppProductResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
    parsed, err := parseInAppProductImportID(req.ID)
    if err != nil {
        resp.Diagnostics.AddError("Invalid Import ID", err.Error())
        return
    }
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("package_name"), parsed.PackageName)...)
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("sku"), parsed.Sku)...)
}
```

Usage: `terraform import googleplay_in_app_product.example "com.example.app/premium_upgrade"`

### Simple Import (PassthroughID)

When the import ID is the same as the resource's `id` attribute, which only applies to a future resource with a single opaque ID of its own (unlike `googleplay_in_app_product`, which has none):

```go
var _ resource.ResourceWithImportState = &fooResource{}

func (r *fooResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
    resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
```

After `ImportState` sets the minimal attributes, Terraform calls Read to fill in the rest.

## State Upgrade

When you change a resource schema in a breaking way, existing state in `.tfstate` files won't match the new schema. State upgraders transform old state to the new format transparently. No resource exists yet, so none has needed one.

### When to Use

- Changing a list block to SingleNestedBlock
- Renaming attributes
- Changing attribute types (e.g., string to int)
- Restructuring nested objects

### Implementation

1. Increment `Version` in the schema:

```go
func (r *inAppProductResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Version: 1, // Was 0, now 1
        // ... current schema ...
    }
}
```

2. Implement `resource.ResourceWithUpgradeState`:

```go
var _ resource.ResourceWithUpgradeState = &inAppProductResource{}

func (r *inAppProductResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
    return map[int64]resource.StateUpgrader{
        0: {
            StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
                // Parse raw JSON from old state format
                var raw map[string]json.RawMessage
                if err := json.Unmarshal(req.RawState.JSON, &raw); err != nil {
                    resp.Diagnostics.AddError("State Upgrade Error",
                        fmt.Sprintf("Unable to parse raw state: %s", err))
                    return
                }

                var packageName string
                _ = json.Unmarshal(raw["package_name"], &packageName)

                state := inAppProductResourceModel{
                    PackageName: types.StringValue(packageName),
                }
                resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
            },
        },
    }
}
```

### Key Points

- The map key is the OLD schema version (upgrade FROM version X)
- `req.RawState.JSON` contains the raw JSON bytes of the old state
- Parse manually: the old state shape does not match your current model struct
- After upgrade, Terraform calls Read to refresh state with current API data
- Multiple upgraders can be chained (0->1, 1->2, etc.)

## Private State

Store provider-internal data that is not visible in plan output. Useful for:

- ETags or version tokens for optimistic concurrency
- Internal identifiers that shouldn't be user-visible
- Cached metadata to avoid extra API calls

Not used anywhere in this provider today; the Google Play API facts gathered so far don't call for one. A resource built on the edits workflow (store listings, tracks, APKs) is the most plausible future user: it could stash an in-progress edit ID here between `Create`/`Update` steps rather than in a user-visible attribute. The pattern, if it's ever needed:

```go
var _ resource.ResourceWithPrivateState = &fooResource{}

// In Create or Update:
resp.Private.SetKey(ctx, "etag", []byte(apiResponse.Etag))

// In Read or Update:
etagBytes, diags := req.Private.GetKey(ctx, "etag")
etag := string(etagBytes)
```

## Writing State

### Full Model Write

Most common: write the entire model struct to state:

```go
resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
```

### Individual Attribute Write

Set a single attribute by path. The compound `ImportState` implementation above uses this to populate `package_name` and `sku` before Terraform's follow-up Read:

```go
resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("package_name"), parsed.PackageName)...)
```

### Removing Resource from State

When Read discovers the resource no longer exists, using the real `isNotFound` helper from `errors.go` (see `references/guides/resource-lifecycle.md`):

```go
product, err := r.client.Inappproducts.Get(packageName, sku).Context(ctx).Do()
if err != nil {
    if isNotFound(err) {
        resp.State.RemoveResource(ctx)
        return
    }
    resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to read in-app product: %s", err))
    return
}
```

This tells Terraform the resource was deleted externally and needs recreation.

## Related Framework References

| File                                           | Contents                          |
| --------------------------------------------------- | -------------------------------------- |
| `framework/resources/import.mdx`               | Import state documentation        |
| `framework/resources/state-upgrade.mdx`        | State upgrade details             |
| `framework/resources/private-state.mdx`        | Private state storage             |
| `framework/resources/state-move.mdx`           | State move between resource types |
| `framework/handling-data/writing-state.mdx`    | Writing to response state         |
| `framework/handling-data/accessing-values.mdx` | Reading from state/plan/config    |
