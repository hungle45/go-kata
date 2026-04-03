package main

import (
	"strings"
	"testing"
	"unicode"
)

// ---------------------------------------------------------------------------
// Table-driven unit tests
// ---------------------------------------------------------------------------

func TestNormalizeHeaderKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		// --- valid canonicalization ---
		{name: "lowercase single word", input: "content-type", want: "Content-Type"},
		{name: "already canonical", input: "Content-Type", want: "Content-Type"},
		{name: "all lowercase multi-word", input: "accept-encoding", want: "Accept-Encoding"},
		{name: "mixed case", input: "x-FORWARDED-for", want: "X-Forwarded-For"},
		{name: "single word lowercase", input: "host", want: "Host"},
		{name: "single word uppercase", input: "HOST", want: "Host"},
		{name: "digits in word", input: "x-http2-push", want: "X-Http2-Push"},
		{name: "digits only word", input: "x-123", want: "X-123"},

		// --- idempotence: normalize(normalize(x)) == normalize(x) ---
		{name: "already normalized is stable", input: "Content-Type", want: "Content-Type"},

		// --- invalid inputs ---
		{name: "empty string", input: "", wantErr: true},
		{name: "space inside", input: "content type", wantErr: true},
		{name: "underscore", input: "content_type", wantErr: true},
		{name: "unicode letter", input: "héader", wantErr: true},
		{name: "special chars", input: "key@value", wantErr: true},
		{name: "leading hyphen", input: "-content-type", wantErr: true},
		{name: "trailing hyphen", input: "content-type-", wantErr: true},
		{name: "consecutive hyphens", input: "content--type", wantErr: true},
		{name: "just a hyphen", input: "-", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc // capture loop variable — required for t.Parallel() inside range
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeHeaderKey(tc.input)

			if tc.wantErr {
				if err == nil {
					t.Errorf("NormalizeHeaderKey(%q) expected error, got %q", tc.input, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("NormalizeHeaderKey(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeHeaderKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormalizeHeaderKey_Idempotent explicitly verifies stability:
// calling the function twice on a valid key yields the same result.
func TestNormalizeHeaderKey_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"content-type",
		"ACCEPT-ENCODING",
		"x-forwarded-for",
		"Host",
	}

	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			first, err := NormalizeHeaderKey(input)
			if err != nil {
				t.Fatalf("first call failed: %v", err)
			}

			second, err := NormalizeHeaderKey(first)
			if err != nil {
				t.Fatalf("second call failed: %v", err)
			}

			if first != second {
				t.Errorf("not idempotent: first=%q second=%q", first, second)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fuzz test
// ---------------------------------------------------------------------------

// FuzzNormalizeHeaderKey feeds arbitrary byte sequences to the sanitizer.
// The fuzz harness asserts three invariants on every generated input:
//
//  1. The function never panics.
//  2. On success, the returned string contains only valid characters.
//  3. The result is idempotent: Normalize(Normalize(x)) == Normalize(x).
func FuzzNormalizeHeaderKey(f *testing.F) {
	// Seed corpus: representative valid and edge-case inputs.
	seeds := []string{
		"content-type",
		"Accept",
		"X-Forwarded-For",
		"host",
		"",
		"-",
		"--",
		"a-b-c",
		"123",
		"x-123-abc",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		result, err := NormalizeHeaderKey(input)
		if err != nil {
			// Invalid input: nothing more to verify.
			return
		}

		// Invariant 1: result contains only valid characters.
		for _, r := range result {
			if !isValidOutputRune(r) {
				t.Errorf("NormalizeHeaderKey(%q) returned %q containing invalid rune %q", input, result, r)
			}
		}

		// Invariant 2: result is title-cased per segment.
		for _, segment := range strings.Split(result, "-") {
			if len(segment) == 0 {
				t.Errorf("NormalizeHeaderKey(%q) returned %q with empty segment", input, result)
				continue
			}
			first := rune(segment[0])
			if !unicode.IsUpper(first) && !unicode.IsDigit(first) {
				t.Errorf("NormalizeHeaderKey(%q) segment %q does not start with upper or digit", input, segment)
			}
		}

		// Invariant 3: idempotent.
		second, err := NormalizeHeaderKey(result)
		if err != nil {
			t.Errorf("NormalizeHeaderKey(%q) second call on canonical %q failed: %v", input, result, err)
			return
		}
		if result != second {
			t.Errorf("not idempotent: NormalizeHeaderKey(%q)=%q, NormalizeHeaderKey(%q)=%q", input, result, result, second)
		}
	})
}

// isValidOutputRune mirrors the allowed character set for output validation.
func isValidOutputRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '-'
}
