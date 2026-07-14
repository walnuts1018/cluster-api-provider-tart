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

package model

import (
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
)

type Result interface {
	isResult()
}

type ResultActiveReconciled struct {
	Finalizer resourcefinalizer.Result
}

type ResultDeleteWaiting struct {
	Deletion machinedeletion.Result
}

type ResultFinalizerReleased struct {
	Deletion  machinedeletion.Result
	Finalizer resourcefinalizer.Result
}

type ResultDeletingIgnored struct{}

func (ResultActiveReconciled) isResult()  {}
func (ResultDeleteWaiting) isResult()     {}
func (ResultFinalizerReleased) isResult() {}
func (ResultDeletingIgnored) isResult()   {}
