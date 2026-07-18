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

package slot

import sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"

type Slot string

const (
	A Slot = "A"
	B Slot = "B"
)

// FailureはSlotの解釈に失敗した理由を閉じた集合で表す。
// go-sumtype:decl Failure
type Failure interface {
	isFailure()
}

// UnknownはA/B以外のSlot値を受け取ったことを表す。
type Unknown struct {
	Value string
}

func (Unknown) isFailure() {}

// Parseは外部表現を検証済みSlotへ変換する。
func Parse(value string) sharedresult.Result[Slot, Failure] {
	slot := Slot(value)
	switch slot {
	case A, B:
		return sharedresult.Success[Slot, Failure](slot)
	case "":
		return sharedresult.Failure[Slot, Failure](Unknown{Value: value})
	}
	return sharedresult.Failure[Slot, Failure](Unknown{Value: value})
}

// Inactiveは反対側のSlotを返す。
func (s Slot) Inactive() sharedresult.Result[Slot, Failure] {
	switch s {
	case A:
		return sharedresult.Success[Slot, Failure](B)
	case B:
		return sharedresult.Success[Slot, Failure](A)
	case "":
		return sharedresult.Failure[Slot, Failure](Unknown{Value: string(s)})
	}
	return sharedresult.Failure[Slot, Failure](Unknown{Value: string(s)})
}
