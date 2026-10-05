# Plan Modification

## Overview

After validation and before apply, Terraform generates a plan describing expected values. Plan modifiers let you:

- Provide known values for computed attributes (reduce "known after apply" noise)
- Mark resources for replacement when in-place update is impossible
- Return diagnostics on planned changes

## Plan Modification Process

1. Null config values get their default value applied
2. If plan differs from state, computed attributes with null config become unknown
3. Attribute plan modifiers run (in schema order)
4. Resource-level plan modifiers run (`ModifyPlan`)

After apply, all state values MUST match planned values or Terraform produces a "Provider produced inconsistent result" error.

## Built-in Attribute Plan Modifiers

Available in `resource/schema/<type>planmodifier` packages:

### UseStateForUnknown

Copies the prior state value into the plan. Use for computed values that don't change after creation (IDs, creation timestamps).

```go
import "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

schema.StringAttribute{
    Computed: true,
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(),
    },
}
```

This provider has no resources yet, so no `id` attribute uses this today. When the first resource is added (illustratively `googleplay_in_app_product`), give its `status` attribute this modifier directly; see `references/guides/schema-design.md` for the attribute shape, since there is no `rsId()`-style helper in this codebase yet.

### RequiresReplace

Forces resource destruction and recreation when the attribute value changes. Use for immutable API fields.

```go
schema.StringAttribute{
    Required: true,
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.RequiresReplace(),
    },
}
```

A hypothetical `googleplay_in_app_product.package_name` and `.sku` would both use this: the Google Play Android Developer API has no operation to move an in-app product to a different app or rename its SKU after creation, so either changing means a different in-app product, not an update. This is the general convention for "missing operations" in this provider: mark the immutable attribute `RequiresReplace`, and document in the resource's description what destroying and recreating actually does.

### RequiresReplaceIf

Conditional replacement based on provider-defined logic:

```go
stringplanmodifier.RequiresReplaceIf(
    func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
        // Only replace if changing from non-empty to different non-empty
        resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsNull()
    },
    "Replace when changing between non-null values",
    "Replace when changing between non-null values",
)
```

Not used anywhere in this provider yet.

### RequiresReplaceIfConfigured

Like RequiresReplace but only triggers if the practitioner explicitly configured the value (not null):

```go
stringplanmodifier.RequiresReplaceIfConfigured()
```

## Available Modifier Packages

Each type has its own package:

| Type    | Package                               | Modifiers                                                                           |
| --------- | ---------------------------------------- | ---------------------------------------------------------------------------------------- |
| String  | `resource/schema/stringplanmodifier`  | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| Bool    | `resource/schema/boolplanmodifier`    | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| Int64   | `resource/schema/int64planmodifier`   | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| Float64 | `resource/schema/float64planmodifier` | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| List    | `resource/schema/listplanmodifier`    | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| Map     | `resource/schema/mapplanmodifier`     | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| Set     | `resource/schema/setplanmodifier`     | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |
| Object  | `resource/schema/objectplanmodifier`  | UseStateForUnknown, RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured |

None of these are in use yet; the table is here for when a resource needs a List, Map, or Set attribute.

## Custom Plan Modifiers

Implement the relevant `planmodifier.<Type>` interface:

```go
type myModifier struct{}

func (m myModifier) Description(_ context.Context) string {
    return "Description for practitioners"
}

func (m myModifier) MarkdownDescription(_ context.Context) string {
    return "Markdown description for practitioners"
}

func (m myModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
    // Access current state, config, plan values:
    //   req.StateValue  - prior state
    //   req.ConfigValue - configuration value
    //   req.PlanValue   - current plan value
    //
    // Modify plan:
    //   resp.PlanValue = types.StringValue("new-value")
    //   resp.RequiresReplace = true
}
```

Not used anywhere in this provider yet; the built-in modifiers above are expected to cover the first resources.

## Resource-Level Plan Modification

Implement `resource.ResourceWithModifyPlan` for cross-attribute logic:

```go
func (r *fooResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
    // Access full plan, state, config
    // Can add diagnostics, mark for replacement, modify plan values

    if req.Plan.Raw.IsNull() {
        // Resource is being destroyed
        return
    }

    var plan fooResourceModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
}
```

Not used anywhere in this provider yet.

## Common Patterns for a Future Resource

### ID attributes (stable after creation)

```go
"id": schema.StringAttribute{
    Computed:            true,
    MarkdownDescription: "The unique ID of this resource.",
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(),
    },
}
```

### Immutable fields (force recreation)

```go
"package_name": schema.StringAttribute{
    Required: true,
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.RequiresReplace(),
    },
}
```

### Fields an endpoint does not echo back immediately

Some Google Play Android Developer API endpoints are eventually consistent: a field set on create may not appear on the very next read, particularly anything that takes effect only after an edit is committed. If a future resource hits this, mark the attribute `Optional: true, Computed: true` with `UseStateForUnknown()`, and only fall back to an empty/zero value when the model's current value is itself unknown, not when it already holds a value the practitioner configured:

```go
switch {
case apiReportedValue != "":
    model.Field = types.StringValue(apiReportedValue)
case model.Field.IsUnknown():
    model.Field = types.StringValue("")
}
```

This keeps whatever the practitioner configured untouched on a fresh Create where the API hasn't caught up yet, while still reflecting reality once it does.

## Related Framework References

| File                                        | Contents                             |
| ---------------------------------------------- | ----------------------------------------- |
| `framework/resources/plan-modification.mdx` | Full plan modification documentation |
| `framework/resources/default.mdx`           | Default values (interact with plan)  |
| `framework/handling-data/schemas.mdx`       | Schema definition                    |
