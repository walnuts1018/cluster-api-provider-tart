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

// Package talos adapts the Talos machinery gRPC client to the small set of
// observations and operations Tart needs, so that generated Talos API types never
// leak into controller or policy packages. See .agents/skills/talos/SKILL.md.
package talos

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// ErrClientUnavailableは接続済みのclientなしでTalos operationが要求されたことを示す。
var ErrClientUnavailable = errors.New("talos client is unavailable")

// Client is a thin wrapper around the Talos machinery gRPC client. It exposes only the
// observations and operations Tart's reconcile and policy packages need.
type Client struct {
	raw *talosclient.Client
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}

// Version is the observed Talos OS version and platform reported by the machine's
// authenticated or maintenance API.
type Version struct {
	Tag      string
	SHA      string
	Platform string
}

// Version fetches the observed Talos OS version from the connected node.
//
// TODO: 深い安全ロジック(schematic比較、health判定、reboot後の再接続判断)は次セッションで
// host/controlplane側のpolicyへ実装する。ここではTalos APIから値を取得するだけに留める。
func (c *Client) Version(ctx context.Context) (Version, error) {
	if c == nil || c.raw == nil {
		return Version{}, ErrClientUnavailable
	}

	resp, err := c.raw.Version(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("get talos version: %w", err)
	}
	messages := resp.GetMessages()
	if len(messages) == 0 {
		return Version{}, fmt.Errorf("get talos version: empty response")
	}
	v := messages[0].GetVersion()
	return Version{
		Tag:      v.GetTag(),
		SHA:      v.GetSha(),
		Platform: messages[0].GetPlatform().GetName(),
	}, nil
}

// dial establishes a gRPC connection to a single Talos endpoint using the given TLS
// configuration. Maintenance connections are TLS-encrypted but not authenticated
// (self-signed, no client certificate); see DialMaintenance. Authenticated connections
// present a client certificate; see DialAuthenticated.
func dial(ctx context.Context, endpoint string, tlsConfig *tls.Config) (*Client, error) {
	raw, err := talosclient.New(ctx,
		talosclient.WithTLSConfig(tlsConfig),
		talosclient.WithEndpoints(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("dial talos endpoint %s: %w", endpoint, err)
	}
	return &Client{raw: raw}, nil
}
