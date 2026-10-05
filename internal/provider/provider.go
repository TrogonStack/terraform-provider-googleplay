package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/api/androidpublisher/v3"
)

var _ provider.Provider = &googlePlayProvider{}

// testAPIClient is set by tests to bypass authentication and inject a client pointed at a fake API.
var testAPIClient *androidpublisher.Service

type googlePlayProvider struct {
	version string
}

type googlePlayProviderModel struct {
	Credentials               types.String `tfsdk:"credentials"`
	ImpersonateServiceAccount types.String `tfsdk:"impersonate_service_account"`
}

func (p *googlePlayProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "googleplay"
	resp.Version = p.version
}

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

func (p *googlePlayProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *googlePlayProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &googlePlayProvider{
			version: version,
		}
	}
}
