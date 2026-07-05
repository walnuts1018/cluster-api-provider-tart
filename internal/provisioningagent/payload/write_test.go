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

package payload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
)

type memoryTarget struct {
	data       []byte
	position   int64
	syncCalls  int
	writeCalls []int
}

func newMemoryTarget(size int) *memoryTarget {
	data := make([]byte, size)
	return &memoryTarget{data: data}
}

func (target *memoryTarget) Read(data []byte) (int, error) {
	count, err := bytes.NewReader(target.data[target.position:]).Read(data)
	target.position += int64(count)
	return count, err
}

func (target *memoryTarget) Write(data []byte) (int, error) {
	target.writeCalls = append(target.writeCalls, len(data))
	copy(target.data[target.position:], data)
	target.position += int64(len(data))
	return len(data), nil
}

func (target *memoryTarget) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		target.position = offset
	case io.SeekCurrent:
		target.position += offset
	case io.SeekEnd:
		target.position = int64(len(target.data)) + offset
	}
	return target.position, nil
}

func (target *memoryTarget) Sync() error {
	target.syncCalls++
	return nil
}

func TestWriteAndVerifyUsesOneMiBChunksAndReportsEveryTenPercent(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte{0x5a}, ChunkSize*2+123)
	target := newMemoryTarget(len(data))
	var progress []int
	err := WriteAndVerify(
		t.Context(),
		target,
		bytes.NewReader(data),
		int64(len(data)),
		digest.FromBytes(data).String(),
		func(_ context.Context, percentage int) error {
			progress = append(progress, percentage)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("WriteAndVerify() error = %v", err)
	}
	if target.syncCalls != 1 {
		t.Fatalf("Sync() calls = %d, want 1", target.syncCalls)
	}
	wantWrites := []int{ChunkSize, ChunkSize, 123}
	if !equal(target.writeCalls, wantWrites) {
		t.Fatalf("Write() sizes = %v, want %v", target.writeCalls, wantWrites)
	}
	wantProgress := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if !equal(progress, wantProgress) {
		t.Fatalf("progress = %v, want %v", progress, wantProgress)
	}
}

func TestWriteAndVerifyRejectsDigestMismatchAfterSync(t *testing.T) {
	t.Parallel()

	data := []byte("payload")
	target := newMemoryTarget(len(data))
	err := WriteAndVerify(
		t.Context(),
		target,
		bytes.NewReader(data),
		int64(len(data)),
		digest.FromString("different").String(),
		nil,
	)
	if err == nil {
		t.Fatal("WriteAndVerify() accepted a digest mismatch")
	}
	if target.syncCalls != 1 {
		t.Fatalf("Sync() calls = %d, want 1", target.syncCalls)
	}
}

func TestWriteAndVerifyStopsWhenProgressReportFails(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte{1}, ChunkSize)
	target := newMemoryTarget(len(data))
	reportErr := errors.New("report failed")
	err := WriteAndVerify(
		t.Context(),
		target,
		bytes.NewReader(data),
		int64(len(data)),
		digest.FromBytes(data).String(),
		func(context.Context, int) error { return reportErr },
	)
	if !errors.Is(err, reportErr) {
		t.Fatalf("WriteAndVerify() error = %v, want %v", err, reportErr)
	}
	if target.syncCalls != 0 {
		t.Fatalf("Sync() calls = %d, want 0", target.syncCalls)
	}
}

func equal(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
