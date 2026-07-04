package artifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

func TestSignAndVerifyManifest(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	validated, err := Validate(validManifest())
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	signature, err := Sign(validated, "release-2026", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	if err := VerifySignature(validated, signature, StaticTrustStore{"release-2026": publicKey}); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
}

func TestVerifySignatureRejectsTampering(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	original, err := Validate(validManifest())
	if err != nil {
		t.Fatalf("Validate() original error = %v", err)
	}
	signature, err := Sign(original, "release-2026", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	tamperedValue := validManifest()
	tamperedValue.Generation++
	tampered, err := Validate(tamperedValue)
	if err != nil {
		t.Fatalf("Validate() tampered error = %v", err)
	}
	if err := VerifySignature(tampered, signature, StaticTrustStore{"release-2026": publicKey}); err == nil {
		t.Fatal("VerifySignature() error = nil, want signature verification error")
	}
}

func TestVerifyPayload(t *testing.T) {
	t.Parallel()

	payload := []byte("immutable OS payload")
	expectedDigest := digest.FromBytes(payload).String()

	tests := []struct {
		name    string
		payload []byte
		digest  string
		size    int64
		wantErr string
	}{
		{
			name:    "valid",
			payload: payload,
			digest:  expectedDigest,
			size:    int64(len(payload)),
		},
		{
			name:    "changed byte",
			payload: []byte("immutable OS payloae"),
			digest:  expectedDigest,
			size:    int64(len(payload)),
			wantErr: "digest mismatch",
		},
		{
			name:    "truncated",
			payload: payload[:len(payload)-1],
			digest:  expectedDigest,
			size:    int64(len(payload)),
			wantErr: "size mismatch",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := VerifyPayload(bytes.NewReader(tt.payload), tt.digest, tt.size)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("VerifyPayload() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("VerifyPayload() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
