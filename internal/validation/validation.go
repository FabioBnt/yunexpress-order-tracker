package validation

import (
	"bufio"
	"errors"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// Conservative: alphanumeric + dash, 8..35 chars (tune to provider spec)
	trackingRe = regexp.MustCompile(`^[A-Za-z0-9-]{8,35}$`)
)

func ValidateTrackingNumber(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("invalid utf-8")
	}
	if strings.ContainsAny(s, "\r\n\t\x00") {
		return errors.New("contains control characters")
	}
	if !trackingRe.MatchString(s) {
		return errors.New("tracking number format invalid")
	}
	return nil
}

// ReadLinesSanitized loads a file into a slice, trimming whitespace,
// skipping empty or commented lines, and validating with an optional validator.
func ReadLinesSanitized(path string, validator func(string) error) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	// Avoid huge lines
	const maxCap = 1024 * 8
	buf := make([]byte, 0, 1024)
	sc.Buffer(buf, maxCap)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsRune(line, '\x00') {
			return nil, errors.New("nul byte in input")
		}
		if validator != nil {
			if err := validator(line); err != nil {
				return nil, err
			}
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
