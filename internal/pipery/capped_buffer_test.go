package pipery

import "testing"

func TestCappedBufferTruncatesWithoutShortWrite(t *testing.T) {
	buffer := newCappedBuffer(5)

	written, err := buffer.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if written != len("hello world") {
		t.Fatalf("expected full write length %d, got %d", len("hello world"), written)
	}

	if got := buffer.String(); got != "hello" {
		t.Fatalf("expected captured payload %q, got %q", "hello", got)
	}

	if !buffer.Truncated() {
		t.Fatalf("expected buffer to report truncation")
	}

	if got := buffer.TotalBytes(); got != len("hello world") {
		t.Fatalf("expected total bytes %d, got %d", len("hello world"), got)
	}
}
