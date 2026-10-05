# Provider-Defined Functions

## Overview

Provider-defined functions (Terraform 1.8+) let practitioners call provider logic directly in expressions. Unlike resources/data sources, functions are pure computations: no state, no side effects.

This provider defines no functions today. Everything below is illustrative, built around a plausible one: splitting a reverse-DNS Android package name (the `package_name` attribute of the hypothetical `googleplay_in_app_product` resource, e.g. `com.example.app`) into its prefix and final segment, so a practitioner could do this in an expression:

```hcl
# Usage in Terraform config (illustrative, this function does not exist):
output "package_suffix" {
  value = provider::googleplay::package_name_suffix("com.example.app").suffix
}
```

## Interface

```go
type Function interface {
    Metadata(context.Context, MetadataRequest, *MetadataResponse)
    Definition(context.Context, DefinitionRequest, *DefinitionResponse)
    Run(context.Context, RunRequest, *RunResponse)
}
```

## Implementation

### Define the Function

```go
package provider

import (
    "context"
    "fmt"
    "strings"

    "github.com/hashicorp/terraform-plugin-framework/function"
    "github.com/hashicorp/terraform-plugin-framework/types"
)

var _ function.Function = &packageNameSuffixFunction{}

func newPackageNameSuffixFunction() function.Function {
    return &packageNameSuffixFunction{}
}

type packageNameSuffixFunction struct{}

func (f *packageNameSuffixFunction) Metadata(_ context.Context, req function.MetadataRequest, resp *function.MetadataResponse) {
    resp.Name = "package_name_suffix"
}

func (f *packageNameSuffixFunction) Definition(_ context.Context, req function.DefinitionRequest, resp *function.DefinitionResponse) {
    resp.Definition = function.Definition{
        Summary:     "Splits a reverse-DNS Android package name into its prefix and final segment",
        Description: "Given a package name like \"com.example.app\", returns an object with prefix (\"com.example\") and suffix (\"app\") attributes.",
        Parameters: []function.Parameter{
            function.StringParameter{
                Name:        "package_name",
                Description: "The Android package name, e.g. \"com.example.app\"",
            },
        },
        Return: function.ObjectReturn{
            AttributeTypes: map[string]attr.Type{
                "prefix": types.StringType,
                "suffix": types.StringType,
            },
        },
    }
}

func (f *packageNameSuffixFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
    var packageName string
    resp.Error = function.ConcatFuncErrors(req.Arguments.Get(ctx, &packageName))
    if resp.Error != nil {
        return
    }

    i := strings.LastIndex(packageName, ".")
    if i < 0 {
        resp.Error = function.NewArgumentFuncError(0, fmt.Sprintf("expected a dotted package name such as \"com.example.app\", got: %q", packageName))
        return
    }

    result, diags := types.ObjectValue(
        map[string]attr.Type{"prefix": types.StringType, "suffix": types.StringType},
        map[string]attr.Value{"prefix": types.StringValue(packageName[:i]), "suffix": types.StringValue(packageName[i+1:])},
    )
    resp.Error = function.ConcatFuncErrors(function.FuncErrorFromDiags(ctx, diags))
    if resp.Error != nil {
        return
    }
    resp.Error = function.ConcatFuncErrors(resp.Result.Set(ctx, result))
}
```

### Register with Provider

Add to the provider's `Functions` method:

```go
var _ provider.ProviderWithFunctions = &googlePlayProvider{}

func (p *googlePlayProvider) Functions(_ context.Context) []func() function.Function {
    return []func() function.Function{
        newPackageNameSuffixFunction,
    }
}
```

`googlePlayProvider` does not implement `provider.ProviderWithFunctions` today; this would be a new addition to `provider.go`, alongside `Resources()` and `DataSources()`.

## Parameter Types

| Parameter Type              | Go Argument Type              |
| ------------------------------ | -------------------------------- |
| `function.StringParameter`  | `string`                      |
| `function.BoolParameter`    | `bool`                         |
| `function.Int64Parameter`   | `int64`                        |
| `function.Float64Parameter` | `float64`                       |
| `function.ListParameter`    | `[]T` or `types.List`         |
| `function.MapParameter`     | `map[string]T` or `types.Map` |
| `function.SetParameter`     | `[]T` or `types.Set`          |
| `function.ObjectParameter`  | struct or `types.Object`      |
| `function.DynamicParameter` | `types.Dynamic`                |

### Variadic Parameter

```go
resp.Definition = function.Definition{
    Parameters: []function.Parameter{
        function.StringParameter{Name: "separator"},
    },
    VariadicParameter: function.StringParameter{
        Name:        "values",
        Description: "Values to join",
    },
    Return: function.StringReturn{},
}

func (f *joinFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
    var separator string
    var values []string
    resp.Error = function.ConcatFuncErrors(req.Arguments.Get(ctx, &separator, &values))
    // ...
}
```

## Return Types

| Return Type              | Go Result Type  |
| --------------------------- | ------------------ |
| `function.StringReturn`  | `string`         |
| `function.BoolReturn`    | `bool`           |
| `function.Int64Return`   | `int64`          |
| `function.Float64Return` | `float64`        |
| `function.ListReturn`    | `types.List`    |
| `function.MapReturn`     | `types.Map`     |
| `function.SetReturn`     | `types.Set`     |
| `function.ObjectReturn`  | `types.Object`  |
| `function.DynamicReturn` | `types.Dynamic` |

## Error Handling

Functions use `function.FuncError` instead of diagnostics:

```go
// Single error
resp.Error = function.NewFuncError("something went wrong")

// Error with argument position
resp.Error = function.NewArgumentFuncError(0, "first argument is invalid")

// Combine errors
resp.Error = function.ConcatFuncErrors(
    req.Arguments.Get(ctx, &arg1, &arg2),
)
```

## Testing Functions

### Unit Tests

```go
func TestPackageNameSuffixFunction(t *testing.T) {
    f := &packageNameSuffixFunction{}

    // Test definition
    defResp := function.DefinitionResponse{}
    f.Definition(context.Background(), function.DefinitionRequest{}, &defResp)
    if defResp.Definition.Summary == "" {
        t.Error("expected non-empty summary")
    }
}
```

### Acceptance Tests

```go
resource.Test(t, resource.TestCase{
    ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
    Steps: []resource.TestStep{
        {
            Config: testProviderConfig + `
output "test" {
  value = provider::googleplay::package_name_suffix("com.example.app").suffix
}
`,
            Check: resource.TestCheckOutput("test", "app"),
        },
    },
})
```

## Related Framework References

| File                                       | Contents               |
| ----------------------------------------------- | ------------------------- |
| `framework/functions/index.mdx`            | Functions overview     |
| `framework/functions/concepts.mdx`         | Concepts and use cases |
| `framework/functions/implementation.mdx`   | Implementation details |
| `framework/functions/testing.mdx`          | Testing functions      |
| `framework/functions/errors.mdx`           | Error handling         |
| `framework/functions/parameters/index.mdx` | All parameter types    |
| `framework/functions/returns/index.mdx`    | All return types       |
