package random_test

import (
	"strconv"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/util/random"
)

func Test_random_SecureString(t *testing.T) {
	type args struct {
		length uint
		base   string
	}
	type want struct {
		f      func(got string) error
		length uint
	}
	//nolint:exhaustruct
	tests := []struct {
		name    string
		args    args
		want    want
		wantErr bool
	}{
		{
			name: "empty base returns error",
			args: args{
				length: 10,
				base:   "",
			},
			want:    want{},
			wantErr: true,
		},
		{
			name: "normal",
			args: args{
				length: 10,
				base:   random.AlphanumericSymbols,
			},
			want:    want{length: 10},
			wantErr: false,
		},
		{
			name: "Numbers",
			args: args{
				length: 16,
				base:   random.Numbers,
			},
			want: want{
				f: func(got string) error {
					_, err := strconv.ParseUint(got, 10, 64)
					return err
				},
				length: 16,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := random.New()

			got, err := r.SecureString(tt.args.length, tt.args.base)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecureString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != int(tt.want.length) {
				t.Errorf("SecureString() = %v, want length %v", got, tt.want.length)
			}

			if tt.want.f != nil {
				if err := tt.want.f(got); err != nil {
					t.Error(err.Error())
				}
			}
		})
	}
}
