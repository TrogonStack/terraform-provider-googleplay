package provider

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/option"
)

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

func TestNewTokenSource_FromCredentialsJSON(t *testing.T) {
	source, err := newTokenSource(t.Context(), clientConfig{CredentialsJSON: newTestServiceAccountJSON(t)})
	if err != nil {
		t.Fatalf("expected a token source, got error: %v", err)
	}
	if source == nil {
		t.Fatal("expected a non-nil token source")
	}
}

func TestNewTokenSource_InvalidCredentialsJSON(t *testing.T) {
	_, err := newTokenSource(t.Context(), clientConfig{CredentialsJSON: "not json"})
	if err == nil || !strings.Contains(err.Error(), "loading service account credentials") {
		t.Fatalf("expected a credential loading error, got %v", err)
	}
}

func TestNewTokenSource_ApplicationDefaultCredentialsError(t *testing.T) {
	// Point ADC lookup at a file that does not exist, so the test never
	// depends on (or reaches) credentials that might be configured on the
	// machine actually running it.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/credentials.json")

	_, err := newTokenSource(t.Context(), clientConfig{})
	if err == nil || !strings.Contains(err.Error(), "finding application default credentials") {
		t.Fatalf("expected an application default credentials error, got %v", err)
	}
}

func TestNewTokenSource_Impersonation(t *testing.T) {
	source, err := newTokenSource(t.Context(), clientConfig{
		CredentialsJSON:           newTestServiceAccountJSON(t),
		ImpersonateServiceAccount: "target@test-project.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("expected an impersonated token source, got error: %v", err)
	}
	if source == nil {
		t.Fatal("expected a non-nil token source")
	}
}

func TestNewAndroidPublisherService_BuildsClient(t *testing.T) {
	service, err := newAndroidPublisherService(t.Context(), clientConfig{CredentialsJSON: newTestServiceAccountJSON(t)})
	if err != nil {
		t.Fatalf("expected the service to build, got error: %v", err)
	}
	if service == nil || service.Edits == nil {
		t.Fatal("expected a usable Android Publisher service")
	}
}

func TestIsNotFound(t *testing.T) {
	service := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{
			"code":    404,
			"message": "Edit was not found",
		}})
	})

	_, err := service.Edits.Get("com.example.app", "missing-edit").Do()
	if !isNotFound(err) {
		t.Fatalf("expected a not-found error, got %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected the error to mention the status code, got %q", err.Error())
	}
	if isNotFound(fmt.Errorf("wrapped: %w", errors.New("plain"))) {
		t.Fatal("expected a non-API error not to count as not found")
	}
	if !isNotFound(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("expected a wrapped API error to still count as not found")
	}
}

func TestIsNotFound_OtherStatusCodes(t *testing.T) {
	service := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{
			"code":    403,
			"message": "The caller does not have permission",
		}})
	})

	_, err := service.Edits.Get("com.example.app", "some-edit").Do()
	if isNotFound(err) {
		t.Fatalf("expected a 403 not to count as not found, got %v", err)
	}
}
