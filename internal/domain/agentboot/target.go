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

package agentboot

import "errors"

var (
	ErrTargetNotFound  = errors.New("agent boot target not found")
	ErrTargetAmbiguous = errors.New("agent boot target is ambiguous")
	ErrUnsupportedHost = errors.New("host does not support the Agent iPXE profile")
)

type Target struct {
	HostUID         string
	OperationUID    string
	BootMACAddress  string
	PlatformProfile string
}
