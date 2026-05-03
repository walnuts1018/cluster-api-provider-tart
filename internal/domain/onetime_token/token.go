/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onetimetoken

import (
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/util/random"
)

const length = 64

// OneTimeToken は Bootstrap Data を一度だけ配信するための不透明なドメイン値です。
type OneTimeToken string

// New は Bootstrap Data の一度きりの配信用に推測困難なトークンを生成します。
func New() (OneTimeToken, error) {
	token, err := random.New().SecureString(length, random.Alphanumeric)
	if err != nil {
		return "", fmt.Errorf("failed to generate one time token: %w", err)
	}
	return OneTimeToken(token), nil
}

// String は CRD status や URL へ渡すための文字列表現を返します。
func (t OneTimeToken) String() string {
	return string(t)
}
