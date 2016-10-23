package cgolib

import "testing"

func TestPure(t *testing.T) {
	if PureCall() != 42 {
		t.Fatalf("c'mon")
	}
}
