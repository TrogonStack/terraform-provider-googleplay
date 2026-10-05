# Validation

## Overview

Validation runs during `terraform validate`, `terraform plan`, and `terraform apply`. It returns diagnostics (warnings/errors) before any API calls happen. Validation occurs at two levels:

1. **Attribute-level validators**: validate individual attribute values
2. **Resource/data-source-level validation**: cross-attribute validation logic

Important: configuration values may be unknown during validation (references to other resources). Validators must handle this by returning early without diagnostics.

This provider has no resources yet, so no attribute validator exists in real code today. Everything below is illustrative, built around a hypothetical `googleplay_in_app_product` resource.

## Attribute Validators

Add validators to any attribute's `Validators` field. All validators in the slice always run (no short-circuit).

```go
import (
    "github.com/hashicorp/terraform-plugin-framework/schema/validator"
    "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
)

schema.StringAttribute{
    Required:            true,
    MarkdownDescription: "The product type. One of `managedUser` (a one-time purchase) or `subscription`.",
    Validators: []validator.String{
        stringvalidator.OneOf("managedUser", "subscription"),
    },
}
```

The `stringvalidator.OneOf` constraint mirrors whatever the generated `androidpublisher.InAppProduct.PurchaseType` field's documented possible values are; the validator gives the practitioner that error earlier, at plan time, instead of waiting for a 4xx response.

## Common Validators (terraform-plugin-framework-validators)

### String

| Validator                                     | Description           |
| ------------------------------------------------- | ------------------------ |
| `stringvalidator.LengthBetween(min, max)`     | String length range   |
| `stringvalidator.LengthAtLeast(min)`          | Minimum length        |
| `stringvalidator.LengthAtMost(max)`           | Maximum length        |
| `stringvalidator.RegexMatches(re, msg)`       | Regex pattern match   |
| `stringvalidator.OneOf("a", "b", "c")`        | Enum values           |
| `stringvalidator.NoneOf("x", "y")`            | Excluded values       |
| `stringvalidator.UTF8LengthBetween(min, max)` | UTF-8 character count |
| `stringvalidator.IsURLWithHTTPS()`            | Valid HTTPS URL       |

### Int64

| Validator                          | Description       |
| -------------------------------------- | -------------------- |
| `int64validator.Between(min, max)` | Range (inclusive) |
| `int64validator.AtLeast(min)`      | Minimum           |
| `int64validator.AtMost(max)`       | Maximum           |
| `int64validator.OneOf(1, 2, 3)`    | Enum values       |

### Bool

| Validator                    | Description  |
| --------------------------------- | -------------- |
| `boolvalidator.Equals(true)` | Must be true |

### List/Set/Map

| Validator                             | Description           |
| ------------------------------------------ | ------------------------ |
| `listvalidator.SizeAtLeast(min)`      | Minimum element count |
| `listvalidator.SizeAtMost(max)`       | Maximum element count |
| `listvalidator.SizeBetween(min, max)` | Element count range   |
| `listvalidator.UniqueValues()`        | No duplicate elements |

`setvalidator` has the same shapes, for a future Set-typed attribute.

## Conflict/Dependency Validators

Express relationships between attributes:

```go
// Exactly one of these must be set
schema.StringAttribute{
    Optional: true,
    Validators: []validator.String{
        stringvalidator.ExactlyOneOf(
            path.MatchRoot("field_a"),
            path.MatchRoot("field_b"),
        ),
    },
}

// At least one of these must be set
schema.StringAttribute{
    Optional: true,
    Validators: []validator.String{
        stringvalidator.AtLeastOneOf(
            path.MatchRoot("field_a"),
            path.MatchRoot("field_b"),
        ),
    },
}

// These conflict (cannot both be set)
schema.StringAttribute{
    Optional: true,
    Validators: []validator.String{
        stringvalidator.ConflictsWith(
            path.MatchRoot("other_field"),
        ),
    },
}

// Required together (if one is set, all must be set)
schema.StringAttribute{
    Optional: true,
    Validators: []validator.String{
        stringvalidator.AlsoRequires(
            path.MatchRoot("other_field"),
        ),
    },
}
```

Not used anywhere in this provider today.

## Custom Validators

Implement the `validator.<Type>` interface. Not used anywhere in this provider today; the shape below is illustrative, for a package name that must look like a reverse-DNS Android package identifier:

```go
type packageNameValidator struct{}

func (v packageNameValidator) Description(_ context.Context) string {
    return "value must be a dotted reverse-DNS package name, e.g. com.example.app"
}

func (v packageNameValidator) MarkdownDescription(_ context.Context) string {
    return "value must be a dotted reverse-DNS package name, e.g. `com.example.app`"
}

func (v packageNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
    if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
        return
    }

    value := req.ConfigValue.ValueString()
    if !strings.Contains(value, ".") {
        resp.Diagnostics.AddAttributeError(
            req.Path,
            "Invalid Package Name",
            fmt.Sprintf("%q is not a valid reverse-DNS package name", value),
        )
    }
}

// Usage:
schema.StringAttribute{
    Required:   true,
    Validators: []validator.String{packageNameValidator{}},
}
```

## Resource-Level Validation

For cross-attribute validation that requires access to multiple fields. Not used anywhere in this provider today; the shape below is illustrative:

```go
var _ resource.ResourceWithValidateConfig = &inAppProductResource{}

func (r *inAppProductResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
    var data inAppProductResourceModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
    if resp.Diagnostics.HasError() {
        return
    }

    // Skip validation if values are unknown (references to other resources)
    if data.PurchaseType.IsUnknown() || data.Sku.IsUnknown() {
        return
    }

    if data.PurchaseType.ValueString() == "subscription" && data.Sku.ValueString() == "" {
        resp.Diagnostics.AddAttributeError(
            path.Root("sku"),
            "Missing Required Field",
            "sku is required when purchase_type is 'subscription'",
        )
    }
}
```

## Diagnostics

### Error vs Warning

```go
// Error: blocks apply
resp.Diagnostics.AddError("Title", "Detail message")

// Warning: allows apply but notifies user
resp.Diagnostics.AddWarning("Title", "Detail message")

// Attribute-specific error (shows path in output)
resp.Diagnostics.AddAttributeError(path.Root("name"), "Title", "Detail")

// Check for errors before continuing
if resp.Diagnostics.HasError() {
    return
}
```

The provider's own `Configure` is the only real code using this today, via `resp.Diagnostics.AddError("Configuration Error", ...)` when the client fails to build, and via `resp.Diagnostics.AddAttributeError(path.Root(attribute), "Unknown Provider Configuration", ...)` for an unknown `credentials` or `impersonate_service_account` value (see `references/guides/provider-configuration.md`). A future resource's CRUD methods would use `resp.Diagnostics.AddError("API Error", ...)` the same way, as shown in `references/guides/resource-lifecycle.md`.

## Related Framework References

| File                                                | Contents                      |
| -------------------------------------------------------- | -------------------------------- |
| `framework/validation.mdx`                          | Full validation documentation |
| `framework/diagnostics.mdx`                         | Diagnostics (errors/warnings) |
| `framework/resources/validate-configuration.mdx`    | Resource-level validation     |
| `framework/data-sources/validate-configuration.mdx` | Data source validation        |
| `framework/providers/validate-configuration.mdx`    | Provider-level validation     |
