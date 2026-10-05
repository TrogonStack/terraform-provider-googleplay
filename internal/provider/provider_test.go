package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/api/androidpublisher/v3"
)

const unknownAttribute = "\x00unknown"

var providerAttributes = []string{"credentials", "impersonate_service_account"}

func configureProvider(t *testing.T, config map[string]string) *provider.ConfigureResponse {
	t.Helper()
	ctx := context.Background()
	p := New("test")()

	schemaResp := &provider.SchemaResponse{}
	p.Schema(ctx, provider.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema has errors: %v", schemaResp.Diagnostics)
	}

	values := map[string]tftypes.Value{}
	for _, name := range providerAttributes {
		if v, ok := config[name]; ok && v == unknownAttribute {
			values[name] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		} else if v, ok := config[name]; ok {
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

func TestProviderConfigure_FromAttributes(t *testing.T) {
	clearCredentialEnv(t)

	resp := configureProvider(t, map[string]string{"credentials": newTestServiceAccountJSON(t)})
	if resp.Diagnostics.HasError() {
		t.Fatalf("expected configure to succeed, got %v", resp.Diagnostics)
	}
	service, ok := resp.ResourceData.(*androidpublisher.Service)
	if !ok || service == nil {
		t.Fatalf("expected an Android Publisher service, got %#v", resp.ResourceData)
	}
	if resp.DataSourceData != resp.ResourceData {
		t.Fatal("expected data sources and resources to share the client")
	}
}

func TestProviderConfigure_FromEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_CREDENTIALS", newTestServiceAccountJSON(t))

	resp := configureProvider(t, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("expected configure to succeed from the environment, got %v", resp.Diagnostics)
	}
	if _, ok := resp.ResourceData.(*androidpublisher.Service); !ok {
		t.Fatalf("expected an Android Publisher service, got %#v", resp.ResourceData)
	}
}

func TestProviderConfigure_Errors(t *testing.T) {
	clearCredentialEnv(t)

	resp := configureProvider(t, map[string]string{"credentials": "not json"})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected configure to fail")
	}
	got := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(got, "loading service account credentials") {
		t.Fatalf("expected an error about loading service account credentials, got %q", got)
	}
}

func TestProviderConfigure_UsesTestClient(t *testing.T) {
	clearCredentialEnv(t)
	testAPIClient = newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	t.Cleanup(func() { testAPIClient = nil })

	resp := configureProvider(t, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("expected the injected client to bypass credentials, got %v", resp.Diagnostics)
	}
	if resp.ResourceData != testAPIClient {
		t.Fatal("expected the injected test client to be used")
	}
}

func TestProviderConfigure_UnknownValues(t *testing.T) {
	t.Setenv("GOOGLE_CREDENTIALS", newTestServiceAccountJSON(t))

	for _, attribute := range providerAttributes {
		t.Run(attribute, func(t *testing.T) {
			resp := configureProvider(t, map[string]string{attribute: unknownAttribute})
			if resp.Diagnostics.ErrorsCount() != 1 {
				t.Fatalf("expected one error for the unknown attribute, got %v", resp.Diagnostics)
			}
			diagnostic, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
			if !ok || !diagnostic.Path().Equal(path.Root(attribute)) {
				t.Fatalf("expected the error on %s, got %v", attribute, resp.Diagnostics)
			}
			if !strings.Contains(diagnostic.Detail(), "only known after apply") {
				t.Fatalf("expected an unknown-value explanation, got %q", diagnostic.Detail())
			}
			if resp.ResourceData != nil {
				t.Fatal("expected no client while a credential is unknown")
			}
		})
	}
}
