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

package gomega_test

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHaveFields(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SourceRepo Suite")
}

var _ = Describe("SourceClient", Ordered, func() {
	type Test struct {
		A           string
		B           int
		C           string
		Time        metav1.Time
		Slice       []string
		NestedSlice []Test
	}

	tests := []struct {
		name      string
		expected  Test
		actual    Test
		wantEqual bool
	}{
		{
			name: "test1",
			expected: Test{
				A: "a",
				B: 1,
			},
			actual: Test{
				A: "a",
				B: 1,
				C: "c",
			},
			wantEqual: true,
		},
		{
			name: "test2",
			expected: Test{
				A: "a",
				B: 1,
			},
			actual: Test{
				A: "a",
				B: 2,
			},
			wantEqual: false,
		},
		{
			name: "time",
			expected: Test{
				Time: metav1.NewTime(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)),
			},
			actual: Test{
				Time: metav1.NewTime(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)),
			},
			wantEqual: true,
		},
		{
			name:     "time should be skipped",
			expected: Test{},
			actual: Test{
				Time: metav1.NewTime(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)),
			},
			wantEqual: true,
		},
		{
			name: "slice",
			expected: Test{
				Slice: []string{"a", "b"},
			},
			actual: Test{
				Slice: []string{"a", "b"},
			},
			wantEqual: true,
		},
		{
			name:     "slice should be skipped",
			expected: Test{},
			actual: Test{
				Slice: []string{"a", "b"},
			},
			wantEqual: true,
		},
	}
	for _, tt := range tests {
		It(tt.name, func() {
			if tt.wantEqual {
				Expect(tt.actual).To(gomega.HaveFields(tt.expected))
			} else {
				Expect(tt.actual).NotTo(gomega.HaveFields(tt.expected))
			}
		})
	}
})
