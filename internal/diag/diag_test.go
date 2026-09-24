package diag

import "testing"

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "pos",
			got:  Pos{Line: 3, Col: 7}.String(),
			want: "3:7",
		},
		{
			name: "error with file",
			got: (&Error{
				File: "main.cero",
				Pos:  Pos{Line: 4, Col: 12},
				Msg:  "expected Int, found Bool",
			}).Error(),
			want: "main.cero:4:12: expected Int, found Bool",
		},
		{
			name: "error without file",
			got: (&Error{
				Pos: Pos{Line: 4, Col: 12},
				Msg: "msg",
			}).Error(),
			want: "4:12: msg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("string = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
