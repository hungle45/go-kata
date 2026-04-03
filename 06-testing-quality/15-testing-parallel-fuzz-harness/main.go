package main

import (
	"errors"
	"strings"
	"unicode"
)

func NormalizeHeaderKey(s string) (string, error) {
	if s == "" {
		return "", errors.New("header key must not be empty")
	}

	for _, r := range s {
		if !isValidRune(r) {
			return "", errors.New("invalid character in header key")
		}
	}

	parts := strings.Split(s, "-")
	for i, part := range parts {
		if part == "" {
			return "", errors.New("invalid header key: empty segment around hyphen")
		}
		parts[i] = titleCase(part)
	}

	return strings.Join(parts, "-"), nil
}

func isValidRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '-'
}

func titleCase(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		if i == 0 {
			runes[i] = unicode.ToUpper(r)
		} else {
			runes[i] = unicode.ToLower(r)
		}
	}
	return string(runes)
}
