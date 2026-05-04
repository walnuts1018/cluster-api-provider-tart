# Copyright 2026.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM alpine:latest AS downloader
RUN apk add --no-cache curl tar
RUN mkdir -p /output/ipxe && \
    curl -fsSL "https://github.com/ipxe/ipxe/releases/download/v2.0.0/ipxeboot.tar.gz" -o /tmp/ipxeboot.tar.gz && \
    tar xzf /tmp/ipxeboot.tar.gz -C /output && \
    rm /tmp/ipxeboot.tar.gz

# Final Stage: Collect both
FROM scratch
COPY --from=downloader /output/ipxe/x86_64/ipxe.efi /ipxe-x86_64.efi
COPY --from=downloader /output/ipxe/arm64/ipxe.efi /ipxe-arm64.efi
