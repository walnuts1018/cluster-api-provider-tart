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

package gomega

import (
	"fmt"
	"reflect"

	"dario.cat/mergo"
	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type HaveFieldsGomegaMatcher[T any] struct {
	expected T
}

func HaveFields[T any](expected T) types.GomegaMatcher {
	return &HaveFieldsGomegaMatcher[T]{
		expected: expected,
	}
}

type timeTransformer struct {
}

func (t timeTransformer) Transformer(typ reflect.Type) func(dst, src reflect.Value) error {
	if typ == reflect.TypeFor[metav1.Time]() {
		return func(dst, src reflect.Value) error {
			if dst.CanSet() {
				isZero := dst.FieldByName("Time").MethodByName("IsZero")
				result := isZero.Call([]reflect.Value{})
				if result[0].Bool() {
					dst.Set(src)
				}
			}
			return nil
		}
	}
	return nil
}

func (a HaveFieldsGomegaMatcher[T]) merged(actual T) T {
	merged := a.expected
	if err := mergo.Merge(&merged, actual, mergo.WithTransformers(timeTransformer{})); err != nil {
		panic(err)
	}
	return merged
}

func (a HaveFieldsGomegaMatcher[T]) Match(actual any) (success bool, err error) {
	t, ok := actual.(T)
	if !ok {
		return false, nil
	}
	return gomega.Equal(a.merged(t)).Match(actual)
}

func (a HaveFieldsGomegaMatcher[T]) FailureMessage(actual any) (message string) {
	t, ok := actual.(T)
	if !ok {
		return "Type assertion failed"
	}
	diff := cmp.Diff(actual, a.merged(t))
	return fmt.Sprintf("diff: \n%s", diff)
}

func (a HaveFieldsGomegaMatcher[T]) NegatedFailureMessage(actual any) (message string) {
	return a.FailureMessage(actual)
}
