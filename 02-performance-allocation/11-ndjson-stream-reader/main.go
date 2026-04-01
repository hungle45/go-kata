package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

func ReadNDJSON(ctx context.Context, r io.Reader, handler func([]byte) error) error {
	br := bufio.NewReader(r)
	currentLine := 1

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("line %d: %w", currentLine, ctx.Err())
		default:
		}

		l, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			for {
				var cl []byte
				cl, err = br.ReadSlice('\n')
				l = append(l, cl...)
				if !errors.Is(err, bufio.ErrBufferFull) {
					break
				}
			}
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("line %d: %w", currentLine, err)
		}

		if len(l) == 0 && errors.Is(err, io.EOF) {
			return nil
		}

		if len(l) > 0 && l[len(l)-1] == '\n' {
			l = l[:len(l)-1]
			if len(l) > 0 && l[len(l)-1] == '\r' {
				l = l[:len(l)-1]
			}
		}

		if handlerErr := handler(l); handlerErr != nil {
			return fmt.Errorf("line %d: %w", currentLine, handlerErr)
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		currentLine++
	}
}

func PrintLineLenHandler(line []byte) error {
	fmt.Printf("Line length: %d\n", len(line))
	return nil
}

func main() {
	f, err := os.ReadFile("very_long_line.txt")
	//f, err := os.ReadFile("short_line.txt")
	if err != nil {
		panic(err)
	}

	err = ReadNDJSON(context.Background(), bytes.NewReader(f), PrintLineLenHandler)
	if err != nil {
		panic(err)
	}
}
