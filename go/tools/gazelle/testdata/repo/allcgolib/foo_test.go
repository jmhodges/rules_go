package allcgolib

import "testing"

func TestingCall(t *testing.T) {
	if CCall() != 42 {
		t.Fatalf("welp")
	}
}
