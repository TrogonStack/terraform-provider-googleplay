terraform {
  required_providers {
    googleplay = {
      source = "trogonstack/googleplay"
    }
  }
}

provider "googleplay" {
  # Omit credentials entirely to use Application Default Credentials,
  # including Workload Identity Federation in CI.
  credentials = file("service-account.json")
}
