package provider

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ForEachLine reads bounded lines without giving a malformed source record an
// opportunity to allocate unbounded memory. Oversize lines are discarded and
// reported to fn with tooLong set.
func ForEachLine(ctx context.Context, path string, maxBytes int, fn func(line []byte, tooLong bool)) error {
	f, err := os.Open(path) // Intentionally read-only: never use OpenFile here.
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 256*1024)
	line := make([]byte, 0, min(maxBytes, 256*1024))
	tooLong := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fragment, prefix, readErr := r.ReadLine()
		if !tooLong {
			if len(line)+len(fragment) > maxBytes {
				line = line[:0]
				tooLong = true
			} else {
				line = append(line, fragment...)
			}
		}
		if !prefix {
			if len(line) > 0 || tooLong {
				fn(line, tooLong)
			}
			line = line[:0]
			tooLong = false
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// TextMetric derives counts and immediately lets the caller discard text.
func TextMetric(s string) (words, characters int) {
	characters = utf8.RuneCountInString(s)
	words = len(strings.FieldsFunc(s, unicode.IsSpace))
	return words, characters
}
