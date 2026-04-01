package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReadNDJSON(t *testing.T) {
	t.Run("short lines", func(t *testing.T) {
		input := "{\"id\":1}\n{\"id\":2}\n{\"id\":3}"
		var lines []string
		err := ReadNDJSON(context.Background(), strings.NewReader(input), func(b []byte) error {
			lines = append(lines, string(b))
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d", len(lines))
		}
		if lines[0] != `{"id":1}` || lines[2] != `{"id":3}` {
			t.Fatalf("unexpected line contents: %v", lines)
		}
	})

	t.Run("very long line", func(t *testing.T) {
		// bufio.NewReader has default size 4096.
		// A 10000 byte string forces ErrBufferFull to occur on ReadSlice
		longStr := strings.Repeat("a", 10000)
		input := longStr + "\n" + `{"id":2}`

		var lines []string
		err := ReadNDJSON(context.Background(), strings.NewReader(input), func(b []byte) error {
			// Copy is needed as bytes are reused
			lines = append(lines, string(b))
			return nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d", len(lines))
		}
		if lines[0] != longStr {
			t.Fatalf("long line did not match")
		}
	})

	t.Run("long line without trailing newline at EOF", func(t *testing.T) {
		longStr := strings.Repeat("b", 10000)
		input := longStr

		var lines []string
		err := ReadNDJSON(context.Background(), strings.NewReader(input), func(b []byte) error {
			lines = append(lines, string(b))
			return nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 1 {
			t.Fatalf("expected 1 line, got %d", len(lines))
		}
		if lines[0] != longStr {
			t.Fatalf("long line did not match")
		}
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		input := "1\n2\n3\n"
		var lines []string
		err := ReadNDJSON(ctx, strings.NewReader(input), func(b []byte) error {
			lines = append(lines, string(b))
			if len(lines) == 2 {
				cancel()
			}
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled error, got %v", err)
		}
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines before cancel, got %d", len(lines))
		}
	})

	t.Run("stops on handler error and wraps with line number", func(t *testing.T) {
		input := "1\n2\n3\n"
		expectedErr := errors.New("handler error")
		var lines []string
		err := ReadNDJSON(context.Background(), strings.NewReader(input), func(b []byte) error {
			lines = append(lines, string(b))
			if len(lines) == 2 {
				return expectedErr
			}
			return nil
		})

		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error to wrap handler error, got %v", err)
		}
		// Error should contain line number context, e.g., "line 2: handler error"
		if !strings.Contains(err.Error(), "line 2") {
			t.Fatalf("expected error to contain line number context 'line 2', got %v", err)
		}
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d", len(lines))
		}
	})

	t.Run("trailing newline should not produce empty line", func(t *testing.T) {
		input := "{\"id\":1}\n"
		var lines []string
		err := ReadNDJSON(context.Background(), strings.NewReader(input), func(b []byte) error {
			lines = append(lines, string(b))
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 1 {
			t.Fatalf("expected 1 line, got %d (contents: %v)", len(lines), lines)
		}
	})

	t.Run("general I/O error bubbling", func(t *testing.T) {
		expectedErr := errors.New("io error")
		err := ReadNDJSON(context.Background(), &errorReader{err: expectedErr}, func(b []byte) error {
			return nil
		})
		// Error should bubble up properly (optionally wrapped with line 1)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected custom I/O error to bubble up, got %v", err)
		}
	})
}

type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}
