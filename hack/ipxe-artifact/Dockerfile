# syntax=docker/dockerfile:1
# Build ipxe.efi (UEFI EFI ROM) from source
# This image contains only the iPXE binary and its license file.
# iPXE is dual-licensed under GPLv2 and MPLv2.0.

FROM alpine:3 AS builder

RUN apk add --no-cache \
    gcc \
    make \
    binutils \
    nasm \
    git

RUN git clone --depth 1 --branch v202501 https://github.com/ipxe/ipxe.git /workspace/ipxe

WORKDIR /workspace/ipxe

# Build the standalone UEFI EFI ROM
# bin/ipxe.efi is the UEFI network boot image
RUN make bin/ipxe.efi -j$(nproc)

# Final image: minimal scratch with only the binary and license
FROM scratch

COPY --from=builder /workspace/ipxe/bin/ipxe.efi /ipxe.efi
COPY --from=builder /workspace/ipxe/licenses/GPLv2.txt /LICENSE
