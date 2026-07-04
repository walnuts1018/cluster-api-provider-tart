package driver

import (
	"errors"
	"testing"
)

func TestDriverErrorClassification(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	err := NewError(ErrorTemporary, cause)
	if !IsErrorKind(err, ErrorTemporary) {
		t.Fatal("IsErrorKind() = false, want true")
	}
	if IsErrorKind(err, ErrorAuthenticationFailed) {
		t.Fatal("temporary error was classified as AuthenticationFailed")
	}
	if !errors.Is(err, cause) {
		t.Fatal("driver error does not unwrap its cause")
	}
}
