package search

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestFindAllInReaderReturnsMatchesAcrossChunkBoundaries(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("xxxxabc123abczzzz"))

	offsets, err := FindAllInReader(reader, int64(reader.Len()), []byte("abc"), 5)
	if err != nil {
		t.Fatalf("FindAllInReader returned error: %v", err)
	}

	expected := []int64{4, 10}
	if !reflect.DeepEqual(offsets, expected) {
		t.Fatalf("FindAllInReader returned %v, want %v", offsets, expected)
	}
}

func TestVerifyAtReportsHitAndMiss(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("0123needle789"))

	matched, err := VerifyAt(reader, 4, []byte("needle"))
	if err != nil {
		t.Fatalf("VerifyAt returned error on hit: %v", err)
	}
	if !matched {
		t.Fatalf("VerifyAt returned false on hit")
	}

	matched, err = VerifyAt(reader, 5, []byte("needle"))
	if err != nil {
		t.Fatalf("VerifyAt returned error on miss: %v", err)
	}
	if matched {
		t.Fatalf("VerifyAt returned true on miss")
	}
}

func TestFindAllInWindowRestrictsMatchesToRange(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("abc---abc---abc"))

	offsets, err := FindAllInWindow(reader, 4, 11, []byte("abc"), 4)
	if err != nil {
		t.Fatalf("FindAllInWindow returned error: %v", err)
	}

	expected := []int64{6}
	if !reflect.DeepEqual(offsets, expected) {
		t.Fatalf("FindAllInWindow returned %v, want %v", offsets, expected)
	}
}

func TestFindAllRejectsEmptyPatternAndInvalidChunkSize(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("abc"))

	_, err := FindAllInReader(reader, int64(reader.Len()), nil, 4)
	if err == nil {
		t.Fatalf("FindAllInReader expected error for empty pattern")
	}
	if !errors.Is(err, ErrEmptyPattern) {
		t.Fatalf("FindAllInReader error = %v, want ErrEmptyPattern", err)
	}

	_, err = FindAllInWindow(reader, 0, int64(reader.Len()), []byte("a"), 0)
	if err == nil {
		t.Fatalf("FindAllInWindow expected error for invalid chunk size")
	}
	if !errors.Is(err, ErrInvalidChunkSize) {
		t.Fatalf("FindAllInWindow error = %v, want ErrInvalidChunkSize", err)
	}
}

func TestVerifyAtRejectsEmptyPattern(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("abc"))

	_, err := VerifyAt(reader, 0, nil)
	if err == nil {
		t.Fatalf("VerifyAt expected error for empty pattern")
	}
	if !errors.Is(err, ErrEmptyPattern) {
		t.Fatalf("VerifyAt error = %v, want ErrEmptyPattern", err)
	}
}

func TestVerifyAtHandlesShortReadAtEndAsMiss(t *testing.T) {
	t.Parallel()

	reader := bytes.NewReader([]byte("abc"))

	matched, err := VerifyAt(reader, 2, []byte("bc"))
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("VerifyAt returned unexpected error: %v", err)
	}
	if matched {
		t.Fatalf("VerifyAt returned true for short trailing read")
	}
}
