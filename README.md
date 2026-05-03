# cluster-api-provider-Tart
Tart is a Kubernetes cluster API provider for local bare-metal desktop PCs. It uses an OS-independent pull-based PXE architecture to enable consistent deployment and operational management of Kubernetes clusters on hardware.

## Network boot bootstrap
The controller manager natively implements ProxyDHCP, TFTP, and HTTP servers to support network booting. It serves iPXE binaries and scripts directly from the controller process, ensuring seamless synchronization with the Kubernetes state.

### Architecture
- **Embedded DHCP Server**: Uses `insomniacslk/dhcp` library for ProxyDHCP mode, providing iPXE bootloader path without IP address distribution.
- **Embedded TFTP Server**: Uses `pin/tftp` library to distribute iPXE binaries and other assets.
- **Embedded HTTP Server**: Serves kernel/initrd images, dynamic iPXE scripts, and secure Bootstrap Data (Secret) using One Time Token.
- **No External Dependencies**: All network boot components run within a single Go binary, eliminating dependencies on external processes like dnsmasq.
