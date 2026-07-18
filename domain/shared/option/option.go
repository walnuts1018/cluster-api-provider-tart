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

package option

// Optionは値が存在する場合と存在しない場合を明示的に表す。
type Option[T any] struct {
	value   T
	present bool
}

// Someは存在する値を包む。
func Some[T any](value T) Option[T] {
	return Option[T]{value: value, present: true}
}

// Noneは値が存在しないことを表す。
func None[T any]() Option[T] {
	return Option[T]{}
}

// IsSomeは値が存在する場合にtrueを返す。
func (option Option[T]) IsSome() bool {
	return option.present
}

// IsNoneは値が存在しない場合にtrueを返す。
func (option Option[T]) IsNone() bool {
	return !option.present
}

// Valueは存在する値と存在有無を返す。
func (option Option[T]) Value() (T, bool) {
	return option.value, option.present
}

// FoldはOptionの両方のvariantを同じ戻り値へ写像する。
func Fold[T, U any](option Option[T], onSome func(T) U, onNone func() U) U {
	if option.present {
		return onSome(option.value)
	}
	return onNone()
}

// Mapは存在する値だけを変換する。
func Map[T, U any](option Option[T], transform func(T) U) Option[U] {
	if option.present {
		return Some(transform(option.value))
	}
	return None[U]()
}
