// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ociremote

import (
	"net"
	"strings"

	"oras.land/oras-go/v2/registry/remote"
)

func AllowLoopbackPlainHTTP(repository *remote.Repository) {
	if repository == nil {
		return
	}
	if IsLoopbackRegistry(repository.Reference.Registry) {
		repository.PlainHTTP = true
	}
}

func IsLoopbackRegistry(registry string) bool {
	host := registry
	if splitHost, _, err := net.SplitHostPort(registry); err == nil {
		host = splitHost
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
