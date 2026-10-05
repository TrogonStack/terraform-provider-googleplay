package provider

import (
	"os"
	"testing"

	"google.golang.org/api/androidpublisher/v3"
)

func requireLivePackageName(t *testing.T) string {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run live acceptance tests against a real Google Play Console account")
	}
	packageName := os.Getenv("GOOGLEPLAY_TEST_PACKAGE_NAME")
	if packageName == "" {
		t.Skip("set GOOGLEPLAY_TEST_PACKAGE_NAME to run live acceptance tests against a real Google Play Console account")
	}
	return packageName
}

// TestLive_Authenticates exercises Application Default Credentials against
// the real API: insert an edit, then delete it, leaving nothing behind.
// Credentials come entirely from the environment (ADC), the same way the
// provider authenticates in GitHub Actions under Workload Identity
// Federation, so this test takes no credentials of its own.
func TestLive_Authenticates(t *testing.T) {
	packageName := requireLivePackageName(t)

	service, err := newAndroidPublisherService(t.Context(), clientConfig{})
	if err != nil {
		t.Fatalf("failed to build a live client: %v", err)
	}

	edit, err := service.Edits.Insert(packageName, &androidpublisher.AppEdit{}).Do()
	if err != nil {
		t.Fatalf("expected an authenticated request to succeed, got: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Edits.Delete(packageName, edit.Id).Do(); err != nil {
			t.Logf("failed to delete the edit created by this test: %v", err)
		}
	})
}
