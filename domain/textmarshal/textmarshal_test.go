package textmarshal

import "testing"

func TestUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantText string
		wantErr  bool
	}{
		{name: "quoted string", input: `"abc"`, wantText: "abc"},
		{name: "empty string", input: `""`, wantText: ""},
		// JSON nullはstrconv.Unquoteでは扱えない非文字列literalであり、空値として
		// unmarshalTextへ渡す契約を検証する(過去に見落とされ、ゼロ値のround-tripが壊れていた)。
		{name: "null", input: `null`, wantText: ""},
		{name: "unquoted literal", input: `abc`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			err := UnmarshalJSON([]byte(tt.input), func(value []byte) error {
				got = string(value)
				return nil
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("UnmarshalJSON(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalJSON(%q) error = %v", tt.input, err)
			}
			if got != tt.wantText {
				t.Errorf("UnmarshalJSON(%q) passed %q, want %q", tt.input, got, tt.wantText)
			}
		})
	}
}

func TestJSON(t *testing.T) {
	got, err := JSON("abc")
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if string(got) != `"abc"` {
		t.Errorf("JSON() = %s, want %q", got, `"abc"`)
	}
}
