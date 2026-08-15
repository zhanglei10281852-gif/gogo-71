package strictjson

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sample struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Ratio float64 `json:"ratio"`
}

func TestUnmarshalAcceptsExactDocument(t *testing.T) {
	var got sample
	if err := Unmarshal([]byte(`{"name":"a","count":3,"ratio":1.5}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := sample{Name: "a", Count: 3, Ratio: 1.5}
	if got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}

func TestUnmarshalRejectsUnknownField(t *testing.T) {
	var got sample
	err := Unmarshal([]byte(`{"name":"a","nickname":"b"}`), &got)
	if err == nil {
		t.Fatal("expected an error for the unknown field")
	}
	if !strings.Contains(err.Error(), "nickname") {
		t.Fatalf("error should name the unknown field, got %v", err)
	}
}

func TestUnmarshalRejectsTrailingValue(t *testing.T) {
	var got sample
	err := Unmarshal([]byte(`{"name":"a"} {"name":"b"}`), &got)
	if err == nil {
		t.Fatal("expected an error for the trailing document")
	}
	if !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnmarshalRejectsEmptyAndMalformed(t *testing.T) {
	var got sample
	for _, in := range []string{"", "   \n\t ", "{", `{"count":"three"}`, `{"count":1.5}`} {
		if err := Unmarshal([]byte(in), &got); err == nil {
			t.Fatalf("expected an error for input %q", in)
		}
	}
}

func TestUnmarshalReportsTypeErrorField(t *testing.T) {
	var got sample
	err := Unmarshal([]byte(`{"count":"three"}`), &got)
	if err == nil {
		t.Fatal("expected a type error")
	}
	if !strings.Contains(err.Error(), "count") {
		t.Fatalf("error should name the offending field, got %v", err)
	}
}

func TestUnmarshalStripsBOMAndRejectsBadUTF8(t *testing.T) {
	var got sample
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"name":"a"}`)...)
	if err := Unmarshal(withBOM, &got); err != nil {
		t.Fatalf("BOM should be tolerated: %v", err)
	}
	if err := Unmarshal([]byte{'{', 0xFF, '}'}, &got); err == nil {
		t.Fatal("expected an error for invalid UTF-8")
	}
}

func TestDecodeFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"disk","count":7,"ratio":0.5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var got sample
	if err := DecodeFile(path, &got); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if got.Name != "disk" || got.Count != 7 {
		t.Fatalf("decoded %+v", got)
	}
	if err := DecodeFile(filepath.Join(dir, "missing.json"), &got); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestDecodeLinesSkipsBlanksAndCountsLines(t *testing.T) {
	input := "\n{\"name\":\"a\"}\n\n{\"name\":\"b\"}\n"
	var names []string
	var lines []int
	err := DecodeLines(strings.NewReader(input), func(lineNo int, raw []byte) error {
		var s sample
		if err := Unmarshal(raw, &s); err != nil {
			return err
		}
		names = append(names, s.Name)
		lines = append(lines, lineNo)
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeLines: %v", err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("decoded names %v", names)
	}
	if lines[0] != 2 || lines[1] != 4 {
		t.Fatalf("line numbers %v, want [2 4]", lines)
	}
}

func TestDecodeLinesRejectsEmptyStreamAndBadRecord(t *testing.T) {
	if err := DecodeLines(strings.NewReader("\n\n"), func(int, []byte) error { return nil }); err == nil {
		t.Fatal("expected an error for a stream with no records")
	}
	err := DecodeLines(strings.NewReader("{\"name\":\"a\"}\n{oops}\n"), func(_ int, raw []byte) error {
		var s sample
		return Unmarshal(raw, &s)
	})
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected a line-qualified error, got %v", err)
	}
}

func TestDecodeLinesRejectsOverlongRecord(t *testing.T) {
	long := "{\"name\":\"" + strings.Repeat("x", MaxLineBytes+16) + "\"}\n"
	err := DecodeLines(strings.NewReader(long), func(int, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "record limit") {
		t.Fatalf("expected a record-limit error, got %v", err)
	}
}

func TestDecodeLinesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.jsonl")
	if err := os.WriteFile(path, []byte("{\"count\":1}\n{\"count\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	total := 0
	if err := DecodeLinesFile(path, func(_ int, raw []byte) error {
		var s sample
		if err := Unmarshal(raw, &s); err != nil {
			return err
		}
		total += s.Count
		return nil
	}); err != nil {
		t.Fatalf("DecodeLinesFile: %v", err)
	}
	if total != 3 {
		t.Fatalf("total %d, want 3", total)
	}
	if err := DecodeLinesFile(filepath.Join(dir, "nope.jsonl"), func(int, []byte) error { return nil }); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestMarshalIsIndentedAndNewlineTerminated(t *testing.T) {
	data, err := Marshal(sample{Name: "x", Count: 1, Ratio: 2})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("output must end with a newline")
	}
	if !bytes.Contains(data, []byte("\n  \"name\": \"x\"")) {
		t.Fatalf("expected two-space indentation, got %s", data)
	}
	if bytes.Contains(data, []byte("\\u00")) {
		t.Fatal("HTML escaping must be disabled")
	}
}

func TestMarshalCompactIsSingleLine(t *testing.T) {
	data, err := MarshalCompact(sample{Name: "x"})
	if err != nil {
		t.Fatalf("MarshalCompact: %v", err)
	}
	if bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("compact output must be a single line, got %q", data)
	}
}

func TestMarshalIsStableAcrossCalls(t *testing.T) {
	value := sample{Name: "stable", Count: 42, Ratio: 0.125}
	first, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("marshalling is not stable on call %d", i)
		}
	}
}

func TestMarshalRejectsUnsupportedValue(t *testing.T) {
	if _, err := Marshal(func() {}); err == nil {
		t.Fatal("expected an error for an unmarshalable value")
	}
	if _, err := MarshalCompact(make(chan int)); err == nil {
		t.Fatal("expected an error for an unmarshalable value")
	}
}
