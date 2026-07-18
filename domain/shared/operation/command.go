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

package operation

import (
	"errors"
	"fmt"
	"time"
)

var ErrUnknownKind = errors.New("unknown TartHostOperation kind")

type Kind string

const (
	KindProvision Kind = "Provision"
	KindUpdate    Kind = "Update"
	KindRollback  Kind = "Rollback"
	KindClean     Kind = "Clean"
	KindWipeAll   Kind = "WipeAll"
	KindRecovery  Kind = "Recovery"
)

func ParseKind(value string) (Kind, error) {
	kind := Kind(value)
	switch kind {
	case KindProvision, KindUpdate, KindRollback, KindClean, KindWipeAll, KindRecovery:
		return kind, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, value)
	}
}

type CleaningPolicy string

const (
	CleaningPolicyUnspecified CleaningPolicy = ""
	CleaningPolicyRetainData  CleaningPolicy = "RetainData"
	CleaningPolicyRetainState CleaningPolicy = "RetainState"
	CleaningPolicyWipeAll     CleaningPolicy = "WipeAll"
)

type Command struct {
	Kind           Kind
	Phase          Phase
	CleaningPolicy CleaningPolicy
	Deadline       time.Time
	Now            time.Time
}

type HostCommand interface {
	isHostCommand()
}

type HostNoop struct{}
type HostMarkProvisioning struct{}
type HostMarkUpdating struct{}
type HostMarkCleaning struct {
	Policy CleaningPolicy
}
type HostMarkAvailable struct{}
type HostMarkRetained struct{}
type HostMarkDetached struct{}
type HostMarkProvisioned struct{}
type HostMarkRecoveryRequired struct{}

func (HostNoop) isHostCommand()                 {}
func (HostMarkProvisioning) isHostCommand()     {}
func (HostMarkUpdating) isHostCommand()         {}
func (HostMarkCleaning) isHostCommand()         {}
func (HostMarkAvailable) isHostCommand()        {}
func (HostMarkRetained) isHostCommand()         {}
func (HostMarkDetached) isHostCommand()         {}
func (HostMarkProvisioned) isHostCommand()      {}
func (HostMarkRecoveryRequired) isHostCommand() {}
