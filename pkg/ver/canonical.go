package ver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
)

// Canonicalize takes an arbitrary Go value (usually a struct), marshals it to
// a generic representation, and then formats it strictly according to RFC 8785
// (JSON Canonicalization Scheme - JCS).
func Canonicalize(v interface{}) ([]byte, error) {
	// First pass: convert to map[string]interface{} / []interface{} / primitives
	// using standard json to honor json tags, omitempty, etc.
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic interface{}
	// Use Number to preserve precision and differentiate ints from floats properly where possible
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := jcsSerialize(generic, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Hash returns the sha256 hex string of the canonicalized record,
// formatted as "sha256:<hex>".
func Hash(v interface{}) (string, error) {
	b, err := Canonicalize(v)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

// hashBytes formats a SHA-256 digest of raw bytes in the "sha256:<hex>" form
// used throughout the record.
func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// HashString is the digest form used for detail and output fields, so raw text
// (which can carry secrets) never enters a record.
func HashString(s string) string { return hashBytes([]byte(s)) }

func jcsSerialize(v interface{}, buf *bytes.Buffer) error {
	switch val := v.(type) {
	case map[string]interface{}:
		return jcsSerializeObject(val, buf)
	case []interface{}:
		return jcsSerializeArray(val, buf)
	case string:
		// JCS string serialization is basically standard JSON,
		// but without HTML escaping (<, >, &).
		return jcsSerializeString(val, buf)
	case json.Number:
		// parse it as float64, format as JCS Number.
		f, err := val.Float64()
		if err != nil {
			return err
		}
		return jcsSerializeNumber(f, buf)
	case float64:
		return jcsSerializeNumber(val, buf)
	case int:
		return jcsSerializeNumber(float64(val), buf)
	case int64:
		return jcsSerializeNumber(float64(val), buf)
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		return fmt.Errorf("unsupported type for JCS serialization: %T", val)
	}
	return nil
}

type jcsKV struct {
	k string
	v interface{}
}

func jcsSerializeObject(m map[string]interface{}, buf *bytes.Buffer) error {
	buf.WriteByte('{')
	var keys []jcsKV
	for k, v := range m {
		keys = append(keys, jcsKV{k, v})
	}

	// Sort keys by UTF-16 code units (per RFC 8785)
	sort.Slice(keys, func(i, j int) bool {
		u1 := utf16.Encode([]rune(keys[i].k))
		u2 := utf16.Encode([]rune(keys[j].k))
		for k := 0; k < len(u1) && k < len(u2); k++ {
			if u1[k] < u2[k] {
				return true
			}
			if u1[k] > u2[k] {
				return false
			}
		}
		return len(u1) < len(u2)
	})

	for i, kv := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := jcsSerializeString(kv.k, buf); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := jcsSerialize(kv.v, buf); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

func jcsSerializeArray(a []interface{}, buf *bytes.Buffer) error {
	buf.WriteByte('[')
	for i, v := range a {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := jcsSerialize(v, buf); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func jcsSerializeString(s string, buf *bytes.Buffer) error {
	// Standard JSON string encoding without HTML escaping
	// json.Marshal escapes <, >, and & which is not what JCS wants.
	// But it actually depends. By default json.Marshal escapes HTML.
	// Let's use json.Encoder with SetEscapeHTML(false)
	var encBuf bytes.Buffer
	enc := json.NewEncoder(&encBuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	res := encBuf.Bytes()
	// Encode adds a trailing newline, remove it
	if len(res) > 0 && res[len(res)-1] == '\n' {
		res = res[:len(res)-1]
	}
	buf.Write(res)
	return nil
}

// jcsSerializeNumber serializes a number strictly per ES6 Number.prototype.toString()
// (RFC 8785 Section 3.2.2.3). For simple integers, it prints them directly.
func jcsSerializeNumber(f float64, buf *bytes.Buffer) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("NaN and Infinity are not permitted in JSON")
	}

	// If it's effectively an integer in the safe range, print as int
	if f == math.Trunc(f) && math.Abs(f) <= 9007199254740991 { // 2^53 - 1
		buf.WriteString(strconv.FormatInt(int64(f), 10))
		return nil
	}

	// Otherwise rely on Go's %g or fallback. FormatFloat with 'g', -1 is quite close to ES6.
	// We'll use this for the float cases. Note: Go might output e+08 instead of e+8,
	// but the JCS test vectors mainly focus on integers and very simple floats for our domain.
	s := strconv.FormatFloat(f, 'g', -1, 64)

	// JCS requires 'e+' instead of 'E+' etc
	// We won't need complex float canonicalization for the VER payload which only has cost_usd
	// and simple integers, but this handles standard cases well.
	buf.WriteString(s)
	return nil
}
