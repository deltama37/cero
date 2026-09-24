package wasm

import (
	"bytes"
	"math"
	"testing"
)

func TestAppendUleb128(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    uint64
		want []byte
	}{
		{name: "0", v: 0, want: []byte{0x00}},
		{name: "127", v: 127, want: []byte{0x7f}},
		{name: "128", v: 128, want: []byte{0x80, 0x01}},
		{name: "624485", v: 624485, want: []byte{0xe5, 0x8e, 0x26}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := appendUleb128(nil, tt.v)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("appendUleb128(%d) = %x, want %x", tt.v, got, tt.want)
			}
		})
	}
}

func TestAppendSleb128(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    int64
		want []byte
	}{
		{name: "0", v: 0, want: []byte{0x00}},
		{name: "-1", v: -1, want: []byte{0x7f}},
		{name: "63", v: 63, want: []byte{0x3f}},
		{name: "64", v: 64, want: []byte{0xc0, 0x00}},
		{name: "-64", v: -64, want: []byte{0x40}},
		{name: "-65", v: -65, want: []byte{0xbf, 0x7f}},
		{
			name: "MaxInt64",
			v:    math.MaxInt64,
			want: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00},
		},
		{
			name: "MinInt64",
			v:    math.MinInt64,
			want: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x7f},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := appendSleb128(nil, tt.v)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("appendSleb128(%d) = %x, want %x", tt.v, got, tt.want)
			}
		})
	}
}
