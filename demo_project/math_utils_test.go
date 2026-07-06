package mathutils

import "testing"

func TestDivide(t *testing.T) {
	// Standard case
	res, err := Divide(10, 2)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if res != 5 {
		t.Fatalf("Expected 5, got: %d", res)
	}

	// Division by zero case (expects an error)
	_, err = Divide(10, 0)
	if err == nil {
		t.Fatal("Expected error when dividing by zero, but got nil")
	}
}
