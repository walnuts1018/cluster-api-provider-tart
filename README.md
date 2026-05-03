# cluster-api-provider-Tart
Tart is a Kubernetes cluster API provider for local bare-metal desktop PCs. It uses an OS-independent pull-based PXE architecture to enable consistent deployment and operational management of Kubernetes clusters on hardware.

## Network boot bootstrap
The controller manager natively implements ProxyDHCP, TFTP, and HTTP servers to support network booting. It serves iPXE binaries and scripts directly from the controller process, ensuring seamless synchronization with the Kubernetes state.
