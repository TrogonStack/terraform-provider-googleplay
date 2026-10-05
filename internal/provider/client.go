package provider

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/option"
)

const playScope = androidpublisher.AndroidpublisherScope

// clientConfig resolves to a token source through one of two credential
// sources, optionally impersonating a service account on top of either one.
type clientConfig struct {
	// CredentialsJSON is a service account key's JSON contents. Empty means
	// fall back to Application Default Credentials.
	CredentialsJSON string
	// ImpersonateServiceAccount, when set, is the email address of a service
	// account to impersonate using the credentials resolved above.
	ImpersonateServiceAccount string
}

// newAndroidPublisherService builds a Google Play Android Developer API
// client whose requests are authenticated per cfg and retried per retry.go.
//
// It passes only the fully assembled *http.Client via option.WithHTTPClient,
// because that option short-circuits every other auth-related option the SDK
// supports: the client given to option.WithHTTPClient must already add its
// own Authorization header.
func newAndroidPublisherService(ctx context.Context, cfg clientConfig) (*androidpublisher.Service, error) {
	tokenSource, err := newTokenSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return androidpublisher.NewService(ctx, option.WithHTTPClient(newRetryableClient(tokenSource)))
}

func newTokenSource(ctx context.Context, cfg clientConfig) (oauth2.TokenSource, error) {
	var baseOpts []option.ClientOption
	if cfg.CredentialsJSON != "" {
		baseOpts = append(baseOpts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(cfg.CredentialsJSON)))
	}

	if cfg.ImpersonateServiceAccount != "" {
		source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
			TargetPrincipal: cfg.ImpersonateServiceAccount,
			Scopes:          []string{playScope},
		}, baseOpts...)
		if err != nil {
			return nil, fmt.Errorf("impersonating %s: %w", cfg.ImpersonateServiceAccount, err)
		}
		return source, nil
	}

	if cfg.CredentialsJSON != "" {
		creds, err := google.CredentialsFromJSONWithTypeAndParams(ctx, []byte(cfg.CredentialsJSON), google.ServiceAccount, google.CredentialsParams{
			Scopes: []string{playScope},
		})
		if err != nil {
			return nil, fmt.Errorf("loading service account credentials: %w", err)
		}
		return creds.TokenSource, nil
	}

	creds, err := google.FindDefaultCredentials(ctx, playScope)
	if err != nil {
		return nil, fmt.Errorf("finding application default credentials: %w", err)
	}
	return creds.TokenSource, nil
}
