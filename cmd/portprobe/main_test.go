package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
		error bool
	}{
		{"y\n", true, false},
		{"NO\n", false, false},
		{"maybe\ny\n", true, false},
		{"", false, true},
	} {
		got, err := confirm(bufio.NewReader(strings.NewReader(tc.input)), &bytes.Buffer{}, "Stop?")
		if got != tc.want || (err != nil) != tc.error {
			t.Errorf("confirm(%q) = %v, %v", tc.input, got, err)
		}
	}
}
