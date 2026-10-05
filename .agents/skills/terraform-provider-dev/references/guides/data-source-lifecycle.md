# Data Source Lifecycle

The provider does not define any data sources today (`DataSources()` in `provider.go` returns an empty slice). Everything below is the pattern to follow when one is added, built around a hypothetical `googleplay_in_app_product` data source (the Google Play Android Developer API's `inappproducts` resource, read with `client.Inappproducts.Get(packageName, sku)`).

## Interface

A data source must implement `datasource.DataSource`:

```go
type DataSource interface {
    Metadata(context.Context, MetadataRequest, *MetadataResponse)
    Schema(context.Context, SchemaRequest, *SchemaResponse)
    Read(context.Context, ReadRequest, *ReadResponse)
}
```

Optional interfaces:

- `datasource.DataSourceWithConfigure`: receive provider client
- `datasource.DataSourceWithValidateConfig`: configuration validation

## Registration

```go
func newInAppProductDataSource() datasource.DataSource { return &inAppProductDataSource{} }

// In provider.go:
func (p *googlePlayProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
    return []func() datasource.DataSource{
        newInAppProductDataSource,
    }
}
```

## Metadata

```go
func (d *inAppProductDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_in_app_product"
}
```

## Schema

Data source schemas use `datasource/schema` package (not `resource/schema`):

```go
import "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

func (d *inAppProductDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "package_name": schema.StringAttribute{Required: true},
            "sku":          schema.StringAttribute{Required: true},
            "status":       schema.StringAttribute{Computed: true},
        },
    }
}
```

Key differences from resource schemas:

- No plan modifiers (no plan phase for data sources)
- No defaults (no apply phase)
- Attributes are either Required (lookup key) or Computed (returned value)
- Optional attributes serve as optional filter criteria

## Configure

Same pattern as resources:

```go
func (d *inAppProductDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    client, ok := req.ProviderData.(*androidpublisher.Service)
    if !ok {
        resp.Diagnostics.AddError("Unexpected DataSource Configure Type",
            fmt.Sprintf("Expected *androidpublisher.Service, got: %T", req.ProviderData))
        return
    }
    d.client = client
}
```

## Read

Contract:

- Read configuration from `req.Config` (the user-provided lookup criteria)
- Perform the API call to find the data
- If not found: add an error diagnostic (data sources must find their target)
- Set all attribute values in `resp.State`

`inappproducts` has a direct get-by-key endpoint, so a data source would call `client.Inappproducts.Get` once, rather than filtering a list client-side:

```go
func (d *inAppProductDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
    var data inAppProductDataSourceModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
    if resp.Diagnostics.HasError() {
        return
    }

    packageName := data.PackageName.ValueString()
    sku := data.Sku.ValueString()

    product, err := d.client.Inappproducts.Get(packageName, sku).Context(ctx).Do()
    if err != nil {
        if isNotFound(err) {
            resp.Diagnostics.AddError("Not Found", fmt.Sprintf("In-app product %q in %q not found", sku, packageName))
            return
        }
        resp.Diagnostics.AddError("API Error", fmt.Sprintf("Unable to read in-app product: %s", err))
        return
    }

    data.Status = types.StringValue(product.Status)
    resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
```

A future data source backed by a list-only endpoint (no get-by-key, e.g. one of the `edits.*` nested resources before a key is known) would instead call the generated `List` method and filter client-side, looping while the response's token-pagination field carries a next-page token, treating "not present in any page" the same way: an error diagnostic, not a state removal.

## Data Sources vs Resources

| Aspect           | Resource                     | Data Source          |
| ------------------ | ------------------------------- | ----------------------- |
| Purpose          | Manage lifecycle (CRUD)      | Read-only lookup     |
| Methods          | Create, Read, Update, Delete | Read only            |
| Import           | Supported                    | N/A                  |
| Plan modifiers   | Yes                          | No                   |
| Defaults         | Yes                          | No                   |
| State management | Full lifecycle               | Refreshed every plan |
| Not found        | RemoveResource (drift)       | Error diagnostic     |

## Related Framework References

| File                                                | Contents                            |
| ------------------------------------------------------ | -------------------------------------- |
| `framework/data-sources/index.mdx`                  | Data source interface, registration |
| `framework/data-sources/configure.mdx`              | Configure method                    |
| `framework/data-sources/validate-configuration.mdx` | Validation                          |
| `framework/data-sources/timeouts.mdx`               | Timeout support                     |
