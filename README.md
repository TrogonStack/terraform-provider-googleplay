# terraform-provider-googleplay

**A Terraform provider for the Google Play Developer API.** It authenticates with a Google Cloud identity and is the foundation for managing Google Play Console configuration as code.

**The provider turns console clicks into reviewed configuration.** App listings, releases, and testing tracks are usually set up by hand in the Google Play Console, which leaves no record a code review or a rollout pipeline can rely on. Expressing them as Terraform configuration puts those changes under review and makes them reproducible across apps.

**Resources are added one at a time, as they are needed.** This release ships the provider configuration, authentication and API client; no resources or data sources are exposed yet.

## Provider configuration

```hcl
provider "googleplay" {
  credentials = file("service-account.json")
}
```

| Attribute                      | Environment variable | Description                                                                                  |
| ------------------------------- | --------------------- | ---------------------------------------------------------------------------------------------- |
| `credentials`                   | `GOOGLE_CREDENTIALS`  | The JSON contents of a Google Cloud service account key. Optional, sensitive.                  |
| `impersonate_service_account`   |                       | Email address of a service account to impersonate using the resolved credentials. Optional.    |

When `credentials` is unset in both configuration and the environment, the provider falls back to [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials), including Workload Identity Federation, so CI can authenticate without a static key.

## Resources

None yet.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, test workflow, and release process.
