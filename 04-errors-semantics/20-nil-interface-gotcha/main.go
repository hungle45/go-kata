package main

import "fmt"

type MyError struct {
	Op string
}

func (e *MyError) Error() string {
	return "an error occurred during " + e.Op
}

// BuggyDoThing returns a typed nil pointer wrapped in the error interface.
// The caller sees err != nil even though no real error occurred.
func BuggyDoThing(fail bool) error {
	var e *MyError
	if fail {
		e = &MyError{Op: "write"}
	}
	return e // typed nil when fail==false: interface{type=*MyError, value=nil}
}

// DoThing is the corrected version: returns a true nil interface on success.
func DoThing(fail bool) error {
	var e *MyError
	if fail {
		e = &MyError{Op: "write"}
	}
	if e == nil {
		return nil
	}
	return e
}

type WrapError struct {
	cause error
}

func (w *WrapError) Error() string { return "wrapped: " + w.cause.Error() }
func (w *WrapError) Unwrap() error { return w.cause }

// WrapDoThing wraps DoThing's error to demonstrate errors.As through layers.
func WrapDoThing(fail bool) error {
	err := DoThing(fail)
	if err == nil {
		return nil
	}
	return &WrapError{cause: err}
}

func main() {
	err := BuggyDoThing(false)
	fmt.Printf("BuggyDoThing(false): err != nil → %v\n", err != nil)
	fmt.Printf("Type/value: %T %#v\n", err, err)

	err = DoThing(false)
	fmt.Printf("DoThing(false):      err != nil → %v\n", err != nil)
}
