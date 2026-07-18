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

package random_test

import (
	"strconv"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/utils/random"
)

func Test_random_InsecureString(t *testing.T) {
	type args struct {
		length uint
		base   string
	}
	//nolint:exhaustruct
	tests := []struct {
		name    string
		args    args
		wantLen uint
		wantErr bool
	}{
		{
			name:    "empty base returns error",
			args:    args{length: 5, base: ""},
			wantErr: true,
		},
		{
			name:    "normal",
			args:    args{length: 8, base: random.Alphanumeric},
			wantLen: 8,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := random.New()
			got, err := r.InsecureString(tt.args.length, tt.args.base)
			if (err != nil) != tt.wantErr {
				t.Errorf("InsecureString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && uint(len(got)) != tt.wantLen {
				t.Errorf("InsecureString() = %v, want length %v", got, tt.wantLen)
			}
		})
	}
}

func Test_random_SecureString(t *testing.T) {
	type args struct {
		length uint
		base   string
	}
	type want struct {
		f      func(got string) error
		length uint
	}
	//nolint:exhaustruct
	tests := []struct {
		name    string
		args    args
		want    want
		wantErr bool
	}{
		{
			name: "empty base returns error",
			args: args{
				length: 10,
				base:   "",
			},
			want:    want{},
			wantErr: true,
		},
		{
			name: "normal",
			args: args{
				length: 10,
				base:   random.AlphanumericSymbols,
			},
			want:    want{length: 10},
			wantErr: false,
		},
		{
			name: "Numbers",
			args: args{
				length: 16,
				base:   random.Numbers,
			},
			want: want{
				f: func(got string) error {
					_, err := strconv.ParseUint(got, 10, 64)
					return err
				},
				length: 16,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := random.New()

			got, err := r.SecureString(tt.args.length, tt.args.base)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecureString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != int(tt.want.length) {
				t.Errorf("SecureString() = %v, want length %v", got, tt.want.length)
			}

			if tt.want.f != nil {
				if err := tt.want.f(got); err != nil {
					t.Error(err.Error())
				}
			}
		})
	}
}
