package compare

import (
	"slices"
	"testing"
)

func TestDigestIsCanonical(t *testing.T) {
	c, err := NewCanonicalizer([]string{"$.cart.id", "$.items[*].at"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, a, b string
		equal      bool
	}{
		{"key order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"whitespace", `{"a": [1, 2]}`, `{"a":[1,2]}`, true},
		{"ignored id", `{"cart":{"id":1,"x":2}}`, `{"cart":{"id":9,"x":2}}`, true},
		{"ignored in array", `{"items":[{"at":1},{"at":2}]}`, `{"items":[{"at":3},{"at":4}]}`, true},
		{"real change", `{"cart":{"id":1,"x":2}}`, `{"cart":{"id":1,"x":3}}`, false},
		{"array order matters", `[1,2]`, `[2,1]`, false},
		{"ignored path absent", `{"cart":{"id":1}}`, `{"cart":{}}`, false},
		{"number text", `{"a":1.0}`, `{"a":1}`, false},
		{"big numbers keep precision", `{"a":12345678901234567890}`, `{"a":12345678901234567891}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := c.Digest([]byte(tt.a))
			if err != nil {
				t.Fatal(err)
			}
			b, err := c.Digest([]byte(tt.b))
			if err != nil {
				t.Fatal(err)
			}
			if (a == b) != tt.equal {
				t.Fatalf("digests equal = %v, want %v", a == b, tt.equal)
			}
		})
	}
}

func TestDigestOfNothingIsEmpty(t *testing.T) {
	c, _ := NewCanonicalizer(nil)
	if d, err := c.Digest(nil); err != nil || d != "" {
		t.Fatalf("digest = %q, %v", d, err)
	}
	if _, err := c.Digest([]byte("nope")); err == nil {
		t.Fatal("accepted invalid JSON")
	}
}

func TestNewCanonicalizerRejectsBadPaths(t *testing.T) {
	if _, err := NewCanonicalizer([]string{"a.b"}); err == nil {
		t.Fatal("accepted a bad path")
	}
}

func TestDiff(t *testing.T) {
	c, _ := NewCanonicalizer([]string{"$.id"})
	got, err := c.Diff(
		[]byte(`{"id":1,"a":1,"gone":1,"n":{"x":1},"l":[1,2],"s":[1]}`),
		[]byte(`{"id":2,"a":2,"new":1,"n":{"x":2},"l":[1,3],"s":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"$.a", "$.gone", "$.l[1]", "$.n.x", "$.new", "$.s"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
