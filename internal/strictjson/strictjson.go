// Package strictjson provides the only JSON entry points used by DebrisLedger.
//
// Every decode is strict: unknown fields are rejected, trailing values after
// the first document are rejected, and empty documents are rejected. Encoding
// is deterministic: two-space indentation, no HTML escaping, and a single
// trailing newline.
package strictjson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// MaxLineBytes caps a single JSONL record. Larger records are rejected rather
// than silently truncated.
const MaxLineBytes = 1 << 20

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Decode reads exactly one JSON document from r into v.
func Decode(r io.Reader, v any) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return Unmarshal(raw, v)
}

// Unmarshal strictly decodes a single JSON document held in raw.
func Unmarshal(raw []byte, v any) error {
	raw = bytes.TrimPrefix(raw, utf8BOM)
	if !utf8.Valid(raw) {
		return errors.New("input is not valid UTF-8")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("empty JSON document")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return decodeError(err)
	}
	return ensureEOF(dec)
}

func ensureEOF(dec *json.Decoder) error {
	var extra json.RawMessage
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("trailing content after JSON document: %w", err)
	}
	trimmed := strings.TrimSpace(string(extra))
	if len(trimmed) > 32 {
		trimmed = trimmed[:32] + "..."
	}
	return fmt.Errorf("unexpected trailing JSON value after the first document: %s", trimmed)
}

func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			field = "(root)"
		}
		return fmt.Errorf("field %s: cannot decode JSON %s into %s (offset %d)",
			field, typeErr.Value, typeErr.Type.String(), typeErr.Offset)
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("malformed JSON at byte offset %d: %w", syntaxErr.Offset, err)
	}
	if errors.Is(err, io.EOF) {
		return errors.New("truncated JSON document")
	}
	return err
}

// DecodeFile strictly decodes the JSON document stored at path.
func DecodeFile(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := Decode(f, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// LineHandler receives the raw bytes of one JSONL record. lineNo is 1-based
// and counts every physical line, including skipped blank ones.
type LineHandler func(lineNo int, raw []byte) error

// DecodeLines walks a JSONL stream, handing each non-blank line to fn. Blank
// lines are tolerated; comment lines are not, because JSONL has no comments.
func DecodeLines(r io.Reader, fn LineHandler) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineBytes)
	lineNo := 0
	records := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if lineNo == 1 {
			line = bytes.TrimPrefix(line, utf8BOM)
			line = bytes.TrimSpace(line)
		}
		if len(line) == 0 {
			continue
		}
		buf := make([]byte, len(line))
		copy(buf, line)
		records++
		if err := fn(lineNo, buf); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("line %d exceeds the %d byte record limit", lineNo+1, MaxLineBytes)
		}
		return fmt.Errorf("scan: %w", err)
	}
	if records == 0 {
		return errors.New("JSONL stream contains no records")
	}
	return nil
}

// DecodeLinesFile is DecodeLines against a file on disk.
func DecodeLinesFile(path string, fn LineHandler) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := DecodeLines(f, fn); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Marshal renders v as deterministic indented JSON terminated by a newline.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), nil
}

// MarshalCompact renders v as a single JSON line terminated by a newline,
// which is the on-disk form used by every JSONL ledger.
func MarshalCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), nil
}
