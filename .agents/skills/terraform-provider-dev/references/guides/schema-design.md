# Schema Design

## Overview

Schemas define the shape of configuration, plan, and state data. Each attribute or block maps to a Go struct field via `tfsdk` tags.

No resource exists in this provider yet. The example below is illustrative, for a hypothetical `googleplay_in_app_product` resource (the Google Play Android Developer API's `inappproducts` resource):

```go
type inAppProductResourceModel struct {
    PackageName types.String `tfsdk:"package_name"`
    Sku         types.String `tfsdk:"sku"`
    Status      types.String `tfsdk:"status"`
}
```

## Attribute Types

### Primitives

| Schema Type               | Go Type         | Notes               |
| -------------------------- | ---------------- | --------------------- |
| `schema.StringAttribute`  | `types.String`  | UTF-8 string        |
| `schema.BoolAttribute`    | `types.Bool`    | true/false          |
| `schema.Int64Attribute`   | `types.Int64`   | 64-bit integer      |
| `schema.Int32Attribute`   | `types.Int32`   | 32-bit integer      |
| `schema.Float64Attribute` | `types.Float64` | 64-bit float        |
| `schema.Float32Attribute` | `types.Float32` | 32-bit float        |
| `schema.NumberAttribute`  | `types.Number`  | Arbitrary precision |

The provider's own `credentials` and `impersonate_service_account` attributes (`provider.go`) are the only real `schema.StringAttribute`s in the codebase today; no resource uses the other primitive types yet.

### Collections

| Schema Type            | Go Type      | Requires      |
| ------------------------ | -------------- | --------------- |
| `schema.ListAttribute` | `types.List` | `ElementType` |
| `schema.MapAttribute`  | `types.Map`  | `ElementType` |
| `schema.SetAttribute`  | `types.Set`  | `ElementType` |

Not used anywhere in this provider yet. A future resource modeling something the API returns as a map keyed by region or language (`InAppProduct.Prices`, a `map[string]Price` keyed by ISO 3166-2 region code, or `InAppProduct.Listings`, a `map[string]InAppProductListing` keyed by BCP-47 language) would likely reach for `schema.MapNestedAttribute` rather than a flat collection, since each value is itself an object.

### Nested Attributes (Protocol v6 only)

| Schema Type                    | Go Type                  | Use Case                |
| --------------------------------- | -------------------------- | -------------------------- |
| `schema.SingleNestedAttribute` | `*nestedModel`           | Single object           |
| `schema.ListNestedAttribute`   | `[]nestedModel`          | Ordered list of objects |
| `schema.MapNestedAttribute`    | `map[string]nestedModel` | Keyed objects           |
| `schema.SetNestedAttribute`    | `[]nestedModel`          | Unique set of objects   |

Not used anywhere in this provider yet:

```go
schema.SingleNestedAttribute{
    Optional: true,
    Attributes: map[string]schema.Attribute{
        "key":   schema.StringAttribute{Required: true},
        "value": schema.StringAttribute{Required: true},
    },
}
```

## Blocks

Blocks are structural containers that appear as HCL blocks (with `{}` syntax). Use blocks for complex nested structures, especially when they can be optional or repeated.

| Schema Type                | Go Type                               | HCL Syntax                              |
| ----------------------------- | ---------------------------------------- | ------------------------------------------ |
| `schema.SingleNestedBlock` | `*nestedModel` (pointer for optional) | `block_name { ... }`                    |
| `schema.ListNestedBlock`   | `[]nestedModel`                       | `block_name { ... }` (repeated)         |
| `schema.SetNestedBlock`    | `[]nestedModel`                       | `block_name { ... }` (unique, repeated) |

Not used anywhere in this provider yet; reach for a block only if a future resource needs an optional, HCL-block-shaped nested structure.

### Blocks vs Nested Attributes

| Use Blocks When                                      | Use Nested Attributes When             |
| -------------------------------------------------------- | ------------------------------------------- |
| Optional complex object (pointer nil = not provided) | Always-present object structure        |
| Matching existing Terraform provider conventions     | New providers (preferred direction)    |
| HCL block syntax feels natural for the structure     | Programmatic, data-oriented structures |

## Attribute Behaviors

### Required, Optional, Computed

| Combination                                    | Meaning                                    |
| ------------------------------------------------- | ---------------------------------------------- |
| `Required: true`                               | User must provide; error if missing        |
| `Optional: true`                               | User may provide; null if omitted          |
| `Computed: true`                               | Provider sets the value; user cannot       |
| `Optional: true, Computed: true`               | User may provide OR provider fills         |
| `Optional: true, Computed: true, Default: ...` | User may provide; known default if omitted |

The provider's `credentials` and `impersonate_service_account` are both `Optional: true` (neither is required: an unset `credentials` just falls back to Application Default Credentials). A hypothetical `googleplay_in_app_product` would mark `package_name` and `sku` as `Required` (the API has no operation to move an in-app product to a different app or rename its SKU after creation) and `status` as `Computed: true`, since it reflects whether the product is active in the store rather than something the caller sets directly.

### Sensitive

```go
schema.StringAttribute{
    Required:  true,
    Sensitive: true, // Value hidden in plan/state output
}
```

The provider's own `credentials` attribute is `Sensitive: true`, since it carries the contents of a service account key.

### Deprecation

```go
schema.StringAttribute{
    Optional:           true,
    DeprecationMessage: "Use 'new_field' instead.",
}
```

Not used anywhere in this provider yet.

## Defaults

Set a known value when the user does not provide one. Requires `Optional: true, Computed: true`.

```go
import "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"

schema.StringAttribute{
    Optional: true,
    Computed: true,
    Default:  stringdefault.StaticString("managedUser"),
}
```

Not used anywhere in this provider yet; the signature above is illustrative, for a hypothetical `googleplay_in_app_product.purchase_type` that defaults to `"managedUser"` (the API's default one-time-purchase type) when not configured.

## Plan Modifiers

Control how attribute values change during planning.

```go
import "github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
import "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

schema.StringAttribute{
    Computed: true,
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(), // ID: stable after creation
    },
}

schema.StringAttribute{
    Required: true,
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.RequiresReplace(), // Immutable: forces recreation
    },
}
```

A hypothetical `googleplay_in_app_product.package_name` and `.sku` would both use `stringplanmodifier.RequiresReplace()`, since changing either means a different in-app product, not an update to the current one. Full details: `references/guides/plan-modification.md`.

## Validators

Constrain acceptable values at plan time.

```go
import "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
import "github.com/hashicorp/terraform-plugin-framework/schema/validator"

schema.StringAttribute{
    Required: true,
    Validators: []validator.String{
        stringvalidator.OneOf("managedUser", "subscription"),
    },
}
```

Not used anywhere in this provider yet; the signature above is illustrative, restricting a hypothetical `googleplay_in_app_product.purchase_type` to the values the `InAppProduct.PurchaseType` field actually accepts. Full details: `references/guides/validation.md`.

## ID Attribute Pattern

This provider has no resources yet, so there is no `helpers.go` and no shared ID-attribute helper. A hypothetical `googleplay_in_app_product` has no separate opaque ID of its own: the API already keys the resource by `package_name` plus `sku`, so there is no computed `id` attribute to add in the first place. For a future resource where the API does return a distinct ID, give it this shape directly:

```go
"id": schema.StringAttribute{
    Computed:            true,
    MarkdownDescription: "The unique ID of this resource.",
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(),
    },
}
```

If a second resource repeats the same shape verbatim, that is the point to extract it into a shared helper in a new `helpers.go`, following the pattern other Terraform providers in this organization use (a function, conventionally named `rsId()`, returning this attribute).

## Accessing Values from Models

```go
// Read plan/config into model
var plan inAppProductResourceModel
resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

// Access primitive values
packageName := plan.PackageName.ValueString()

// Check null/unknown
if plan.Sku.IsNull() { /* user did not set */ }
if plan.Status.IsUnknown() { /* will be known after apply */ }

// Set values
plan.Status = types.StringValue("active")
```

## Related Framework References

| File                                                   | Contents                              |
| ---------------------------------------------------------- | ------------------------------------------ |
| `framework/handling-data/schemas.mdx`                  | Schema definition fundamentals        |
| `framework/handling-data/attributes/index.mdx`         | All attribute types overview          |
| `framework/handling-data/attributes/string.mdx`        | String attribute details              |
| `framework/handling-data/attributes/list-nested.mdx`   | List nested attribute                 |
| `framework/handling-data/attributes/single-nested.mdx` | Single nested attribute               |
| `framework/handling-data/blocks/index.mdx`             | Block types overview                  |
| `framework/handling-data/blocks/single-nested.mdx`     | SingleNestedBlock details             |
| `framework/handling-data/types/index.mdx`              | Go value types                        |
| `framework/handling-data/accessing-values.mdx`         | Reading values from state/plan/config |
| `framework/handling-data/writing-state.mdx`            | Writing values to state               |
| `framework/resources/default.mdx`                      | Default values                        |
