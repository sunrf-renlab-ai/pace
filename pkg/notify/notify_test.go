package notify

import "testing"

func TestNotifierConstructs(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New returned nil")
	}
}
