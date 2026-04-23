package main

import (
	"errors"
	"testing"
)

// TestBuggyDoThing proves the typed-nil trap: a typed nil pointer returned as
// error produces a non-nil interface even though no error occurred.
//
// Extra subtlety: errors.As still matches the type (returns true) but leaves
// me == nil, so the safe extraction idiom is to check me != nil after As.
func TestBuggyDoThing(t *testing.T) {
	tests := []struct {
		name       string
		fail       bool
		wantNonNil bool // deliberately documents the trap
		wantMeNil  bool // me == nil after errors.As (typed-nil vs real pointer)
		wantOp     string
	}{
		{
			name:       "trap: typed nil — err != nil is true, me is nil after As",
			fail:       false,
			wantNonNil: true, // BUG: should be false but typed nil makes it true
			wantMeNil:  true, // errors.As matches type but underlying ptr is nil
		},
		{
			name:       "real error: interface and pointer both non-nil",
			fail:       true,
			wantNonNil: true,
			wantMeNil:  false,
			wantOp:     "write",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := BuggyDoThing(tc.fail)

			if got := err != nil; got != tc.wantNonNil {
				t.Errorf("err != nil = %v, want %v", got, tc.wantNonNil)
			}

			// errors.As always returns true here (type matches), but me may be nil.
			var me *MyError
			errors.As(err, &me)
			if got := me == nil; got != tc.wantMeNil {
				t.Errorf("me == nil = %v, want %v", got, tc.wantMeNil)
			}
			if !tc.wantMeNil && me.Op != tc.wantOp {
				t.Errorf("Op = %q, want %q", me.Op, tc.wantOp)
			}
		})
	}
}

// TestDoThing proves the fix: returning literal nil produces a true nil interface.
func TestDoThing(t *testing.T) {
	tests := []struct {
		name        string
		fail        bool
		wantErr     bool
		wantMyError bool
		wantOp      string
	}{
		{
			name:    "fix: no error returns true nil interface",
			fail:    false,
			wantErr: false,
		},
		{
			name:        "real error is still propagated correctly",
			fail:        true,
			wantErr:     true,
			wantMyError: true,
			wantOp:      "write",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := DoThing(tc.fail)

			if got := err != nil; got != tc.wantErr {
				t.Errorf("err != nil = %v, want %v", got, tc.wantErr)
			}

			if !tc.wantErr {
				return
			}

			var me *MyError
			if !errors.As(err, &me) {
				t.Fatal("errors.As: expected *MyError, got none")
			}
			if me == nil {
				t.Fatal("errors.As: me is nil after extraction")
			}
			if me.Op != tc.wantOp {
				t.Errorf("Op = %q, want %q", me.Op, tc.wantOp)
			}
		})
	}
}

// TestWrapDoThing verifies errors.As works through wrapping layers.
func TestWrapDoThing(t *testing.T) {
	tests := []struct {
		name        string
		fail        bool
		wantErr     bool
		wantMyError bool
		wantOp      string
	}{
		{
			name:    "no error through wrapping layers",
			fail:    false,
			wantErr: false,
		},
		{
			name:        "errors.As extracts *MyError through WrapError",
			fail:        true,
			wantErr:     true,
			wantMyError: true,
			wantOp:      "write",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := WrapDoThing(tc.fail)

			if got := err != nil; got != tc.wantErr {
				t.Errorf("err != nil = %v, want %v", got, tc.wantErr)
			}

			if !tc.wantErr {
				return
			}

			var me *MyError
			if !errors.As(err, &me) {
				t.Fatal("errors.As: expected *MyError through wrap, got none")
			}
			if me == nil {
				t.Fatal("errors.As: me is nil after extraction")
			}
			if me.Op != tc.wantOp {
				t.Errorf("Op = %q, want %q", me.Op, tc.wantOp)
			}
		})
	}
}
