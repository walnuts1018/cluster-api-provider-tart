# cluster-api-provider-Tart

Tart is a Kubernetes cluster API provider for local bare-metal desktop PCs. It uses an OS-independent pull-based PXE architecture to enable consistent deployment and operational management of Kubernetes clusters on hardware.

## Installation

See [installation.md](./installation.md) for detailed installation instructions.

## Development

### envtest setup

The local controller test suite uses `envtest` with binaries stored under `bin/k8s`.
Run `mise run setup-envtest` before `mise run test` to download the required assets into that directory.
If you already have envtest binaries available elsewhere, set `KUBEBUILDER_ASSETS` explicitly instead.

The controller test suite also uses repository fixtures for the Cluster API CRDs under `test/envtest/crds`, so the normal controller envtest flow does not fetch CRDs from GitHub during test setup.

### Local e2e

Local e2e workflows are not part of the normal developer loop in this repository.
Use the GitHub Actions workflows for `mise run test-e2e` and `mise run test-provisioning-e2e` instead of running them on your workstation.
