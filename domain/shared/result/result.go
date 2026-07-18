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

package result

import "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/option"

// Resultは成功値または失敗値のどちらか一方を保持する。
type Result[T, F any] struct {
	value       T
	failure     F
	initialized bool
	succeeded   bool
}

// Successは成功値を持つResultを作る。
func Success[T, F any](value T) Result[T, F] {
	return Result[T, F]{value: value, initialized: true, succeeded: true}
}

// Failureは失敗値を持つResultを作る。
func Failure[T, F any](failure F) Result[T, F] {
	return Result[T, F]{failure: failure, initialized: true}
}

// IsSuccessは成功variantの場合にtrueを返す。
func (result Result[T, F]) IsSuccess() bool {
	return result.initialized && result.succeeded
}

// IsFailureは失敗variantの場合にtrueを返す。
func (result Result[T, F]) IsFailure() bool {
	return result.initialized && !result.succeeded
}

// Valueは成功値をOptionとして返す。
func (result Result[T, F]) Value() option.Option[T] {
	if result.IsSuccess() {
		return option.Some(result.value)
	}
	return option.None[T]()
}

// FailureValueは失敗値をOptionとして返す。
func (result Result[T, F]) FailureValue() option.Option[F] {
	if result.IsFailure() {
		return option.Some(result.failure)
	}
	return option.None[F]()
}

// Foldは成功と失敗の両variantを同じ戻り値へ写像する。
func Fold[T, F, U any](result Result[T, F], onSuccess func(T) U, onFailure func(F) U) U {
	if !result.initialized {
		panic("uninitialized Result")
	}
	if result.succeeded {
		return onSuccess(result.value)
	}
	return onFailure(result.failure)
}

// Mapは成功値だけを変換する。
func Map[T, F, U any](result Result[T, F], transform func(T) U) Result[U, F] {
	if result.IsSuccess() {
		return Success[U, F](transform(result.value))
	}
	if result.IsFailure() {
		return Failure[U](result.failure)
	}
	panic("uninitialized Result")
}

// Bindは成功値を次の失敗しうる処理へ渡す。
func Bind[T, F, U any](result Result[T, F], next func(T) Result[U, F]) Result[U, F] {
	if result.IsSuccess() {
		return next(result.value)
	}
	if result.IsFailure() {
		return Failure[U](result.failure)
	}
	panic("uninitialized Result")
}
