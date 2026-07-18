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

package resourcefinalizer

import "fmt"

type Name struct {
	value string
}

func ParseName(value string) (Name, error) {
	if value == "" {
		return Name{}, fmt.Errorf("resource finalizer name must not be empty")
	}
	return Name{value: value}, nil
}

func MustName(value string) Name {
	name, err := ParseName(value)
	if err != nil {
		panic(err)
	}
	return name
}

func (name Name) String() string {
	return name.value
}
