# cluster-api-provider-Tart
Tart is a Kubernetes cluster API provider for local bare-metal desktop PCs. It uses an OS-independent pull-based PXE architecture to enable consistent deployment and operational management of Kubernetes clusters on hardware.

## Network boot bootstrap
`config/bootstrap` contains the minimal ProxyDHCP and TFTP manifests for dnsmasq. The controller manager exposes a placeholder iPXE script endpoint on `--ipxe-bind-address` and serves a dummy script from `/ipxe`.
