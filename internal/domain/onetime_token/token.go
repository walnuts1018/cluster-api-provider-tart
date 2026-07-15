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

package onetimetoken

import (
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/util/random"
)

const length = 64

type Token struct {
	value string
}

func New() (Token, error) {
	token, err := random.New().SecureString(length, random.Alphanumeric)
	if err != nil {
		return Token{}, fmt.Errorf("generate one time token: %w", err)
	}
	return Token{value: token}, nil
}

func Parse(value string) (Token, error) {
	if value == "" {
		return Token{}, fmt.Errorf("one time token must not be empty")
	}
	return Token{value: value}, nil
}

func (token Token) String() string {
	return token.value
}
