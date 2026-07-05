package payload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
)

const ChunkSize = 1 << 20

type SyncReadWriteSeeker interface {
	io.Reader
	io.Writer
	io.Seeker
	Sync() error
}

type ProgressFunc func(context.Context, int) error

// WriteAndVerifyは1 MiB単位でpayloadを書き、fsync後のread-back digestを検証する。
func WriteAndVerify(
	ctx context.Context,
	target SyncReadWriteSeeker,
	source io.Reader,
	sizeBytes int64,
	expectedDigest string,
	report ProgressFunc,
) error {
	if sizeBytes <= 0 {
		return errors.New("payload size must be greater than zero")
	}
	if err := writePayload(ctx, target, source, sizeBytes, report); err != nil {
		return err
	}
	if err := target.Sync(); err != nil {
		return fmt.Errorf("sync target: %w", err)
	}
	if err := verifyPayload(target, sizeBytes, expectedDigest); err != nil {
		return err
	}
	return nil
}

func writePayload(
	ctx context.Context,
	target io.Writer,
	source io.Reader,
	sizeBytes int64,
	report ProgressFunc,
) error {
	buffer := make([]byte, ChunkSize)
	var written int64
	nextProgress := 10
	for written < sizeBytes {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := min(int64(len(buffer)), sizeBytes-written)
		if _, err := io.ReadFull(source, buffer[:chunk]); err != nil {
			return fmt.Errorf("read payload at byte %d: %w", written, err)
		}
		if _, err := io.Copy(target, bytes.NewReader(buffer[:chunk])); err != nil {
			return fmt.Errorf("write payload at byte %d: %w", written, err)
		}
		written += chunk
		progress := int(written * 100 / sizeBytes)
		for report != nil && nextProgress <= progress {
			if err := report(ctx, nextProgress); err != nil {
				return fmt.Errorf("report write progress: %w", err)
			}
			nextProgress += 10
		}
	}
	var trailing [1]byte
	if count, err := source.Read(trailing[:]); count != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		if err != nil {
			return fmt.Errorf("check payload length: %w", err)
		}
		return errors.New("payload exceeds declared size")
	}
	return nil
}

func verifyPayload(target io.ReadSeeker, sizeBytes int64, expectedDigest string) error {
	if _, err := target.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek target for verification: %w", err)
	}
	hasher := sha256.New()
	written, err := io.CopyN(hasher, target, sizeBytes)
	if err != nil {
		return fmt.Errorf("read back target at byte %d: %w", written, err)
	}
	actual := digest.NewDigest(digest.SHA256, hasher).String()
	if actual != expectedDigest {
		return fmt.Errorf("read-back digest mismatch: expected %s, got %s", expectedDigest, actual)
	}
	return nil
}
