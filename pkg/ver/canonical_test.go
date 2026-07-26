package ver

import (
	"encoding/json"
	"testing"
)

func TestCanonicalize(t *testing.T) {
	// Golden vector from an arbitrary JSON to prove JCS rules
	// For instance, keys must be ordered lexicographically.
	// Whitespace must be removed.
	rawJSON := `{
		"c": "value3",
		"a": "value1",
		"b": "value2"
	}`

	var v interface{}
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	canonical, err := Canonicalize(v)
	if err != nil {
		t.Fatalf("canonicalize error: %v", err)
	}

	expected := `{"a":"value1","b":"value2","c":"value3"}`
	if string(canonical) != expected {
		t.Errorf("expected %q, got %q", expected, string(canonical))
	}
}

func TestCanonicalize_FieldReorder(t *testing.T) {
	// Prove that identical content with different field orders in Go structs/maps
	// produces the exact same hash.
	struct1 := struct {
		B string `json:"b"`
		A string `json:"a"`
	}{B: "2", A: "1"}

	struct2 := struct {
		A string `json:"a"`
		B string `json:"b"`
	}{A: "1", B: "2"}

	hash1, err := Hash(struct1)
	if err != nil {
		t.Fatal(err)
	}

	hash2, err := Hash(struct2)
	if err != nil {
		t.Fatal(err)
	}

	if hash1 != hash2 {
		t.Errorf("hashes do not match for reordered structs: %s vs %s", hash1, hash2)
	}

	// Verify the hash is correct for `{"a":"1","b":"2"}`
	// sha256 of `{"a":"1","b":"2"}` is:
	// 21f76dfbfe6dfe21f762080ef484112cf2952974cef30741fd1931e1c6d92112
	expectedHash := "sha256:21f76dfbfe6dfe21f762080ef484112cf2952974cef30741fd1931e1c6d92112"
	if hash1 != expectedHash {
		t.Errorf("expected hash %q, got %q", expectedHash, hash1)
	}
}

func TestCanonicalize_Number(t *testing.T) {
	// Test floats and ints
	val := map[string]interface{}{
		"cost": 0.41,
		"int":  42,
	}
	canonical, err := Canonicalize(val)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"cost":0.41,"int":42}`
	if string(canonical) != expected {
		t.Errorf("expected %q, got %q", expected, string(canonical))
	}
}

func TestCanonicalize_UnicodeEscaping(t *testing.T) {
	// Test that JSON characters are not HTML escaped
	val := map[string]interface{}{
		"html": "<script> & </script>",
	}
	canonical, err := Canonicalize(val)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"html":"<script> & </script>"}`
	if string(canonical) != expected {
		t.Errorf("expected %q, got %q", expected, string(canonical))
	}
}
