package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/oauth2"
)

func TestRetryPolicy_429_Retries(t *testing.T) {
	resp := &http.Response{StatusCode: 429}
	retry, err := retryPolicy(context.Background(), resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retry {
		t.Error("expected retry on 429")
	}
}

func TestRetryPolicy_500_Retries(t *testing.T) {
	resp := &http.Response{StatusCode: 500}
	retry, err := retryPolicy(context.Background(), resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retry {
		t.Error("expected retry on 500")
	}
}

func TestRetryPolicy_502_Retries(t *testing.T) {
	resp := &http.Response{StatusCode: 502}
	retry, err := retryPolicy(context.Background(), resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retry {
		t.Error("expected retry on 502")
	}
}

func TestRetryPolicy_501_DoesNotRetry(t *testing.T) {
	resp := &http.Response{StatusCode: 501}
	retry, err := retryPolicy(context.Background(), resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retry {
		t.Error("expected no retry on 501")
	}
}

func TestRetryPolicy_200_DoesNotRetry(t *testing.T) {
	resp := &http.Response{StatusCode: 200}
	retry, err := retryPolicy(context.Background(), resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retry {
		t.Error("expected no retry on 200")
	}
}

func TestRetryPolicy_404_DoesNotRetry(t *testing.T) {
	resp := &http.Response{StatusCode: 404}
	retry, err := retryPolicy(context.Background(), resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retry {
		t.Error("expected no retry on 404")
	}
}

func TestRetryPolicy_ConnectionError_Retries(t *testing.T) {
	retry, err := retryPolicy(context.Background(), nil, io.ErrUnexpectedEOF)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retry {
		t.Error("expected retry on connection error")
	}
}

func TestRetryPolicy_CancelledContext_DoesNotRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retry, err := retryPolicy(ctx, nil, nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if retry {
		t.Error("expected no retry on cancelled context")
	}
}

func TestRetryPolicy_PostNeverRetries(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestMethodKey{}, http.MethodPost)
	for name, tc := range map[string]struct {
		resp *http.Response
		err  error
	}{
		"server error":    {resp: &http.Response{StatusCode: 500}},
		"rate limited":    {resp: &http.Response{StatusCode: 429}},
		"transport error": {err: io.ErrUnexpectedEOF},
	} {
		t.Run(name, func(t *testing.T) {
			retry, err := retryPolicy(ctx, tc.resp, tc.err)
			if err != nil || retry {
				t.Fatalf("expected POST to never retry, got retry=%v err=%v", retry, err)
			}
		})
	}
}

func TestRetryableClient_BoundsEachAttempt(t *testing.T) {
	source := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})
	tagging, ok := newRetryableClient(source).Transport.(methodTaggingTransport)
	if !ok {
		t.Fatalf("expected the method tagging transport, got %T", newRetryableClient(source).Transport)
	}
	transport, ok := tagging.next.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected the retryable round tripper, got %T", tagging.next)
	}
	if got := transport.Client.HTTPClient.Timeout; got != requestAttemptTimeout {
		t.Fatalf("expected a %s per-attempt timeout, got %s", requestAttemptTimeout, got)
	}
}

func newRetryingTestClient(t *testing.T, handler func(attempt int, w http.ResponseWriter)) (*http.Client, string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		attempt := len(methods)
		mu.Unlock()
		handler(attempt, w)
	}))
	t.Cleanup(server.Close)

	source := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})
	return newRetryableClient(source), server.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), methods...)
	}
}

func unavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "0")
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{
		"code":    503,
		"message": "The service is temporarily unavailable.",
	}})
}

func TestRetryableClient_DoesNotRetryPost(t *testing.T) {
	client, url, requests := newRetryingTestClient(t, func(_ int, w http.ResponseWriter) { unavailable(w) })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected a response rather than a transport error, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected the 503 to surface, got %d", resp.StatusCode)
	}
	if got := len(requests()); got != 1 {
		t.Fatalf("expected exactly one POST attempt, got %d", got)
	}
}

func TestRetryableClient_RetriesIdempotentMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			client, url, requests := newRetryingTestClient(t, func(attempt int, w http.ResponseWriter) {
				if attempt == 1 {
					unavailable(w)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})

			req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
			if err != nil {
				t.Fatalf("failed to build request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("expected the retry to succeed, got %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("expected the retry to succeed, got status %d", resp.StatusCode)
			}
			if got := len(requests()); got != 2 {
				t.Fatalf("expected a retry, got %d attempts", got)
			}
		})
	}
}

func TestRetryableClient_ExhaustsAfterFiveRetries(t *testing.T) {
	client, url, requests := newRetryingTestClient(t, func(_ int, w http.ResponseWriter) { unavailable(w) })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected the final response rather than a transport error, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected the final 503 to surface, got %d", resp.StatusCode)
	}
	if got := len(requests()); got != 6 {
		t.Fatalf("expected the initial attempt plus 5 retries, got %d", got)
	}
}
