package pipery

import "bytes"

// cappedBuffer captures up to a fixed number of bytes and then silently stops
// storing more data.
//
// This is useful for logs because stdout/stderr/stdin can become huge. We want
// to preserve a useful sample without letting one noisy command consume
// unbounded memory.
type cappedBuffer struct {
	buf        bytes.Buffer
	limit      int
	totalBytes int
	truncated  bool
}

// newCappedBuffer creates a buffer with a maximum capture size.
func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

// Write satisfies io.Writer.
//
// The important subtlety here is that we report the original input length even
// when we only keep part of it. That tells callers "I successfully consumed
// your bytes" and prevents io.MultiWriter/io.Copy from treating truncation as a
// short write error.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	originalLen := len(p)
	b.totalBytes += len(p)

	if b.limit <= 0 {
		// A zero or negative limit means "capture nothing, but still track that
		// data existed".
		if len(p) > 0 {
			b.truncated = true
		}
		return originalLen, nil
	}

	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		// We already hit the limit earlier, so from this point on we only record
		// that truncation happened.
		if len(p) > 0 {
			b.truncated = true
		}
		return originalLen, nil
	}

	if len(p) > remaining {
		// Only store the part that still fits.
		b.truncated = true
		p = p[:remaining]
	}

	_, err := b.buf.Write(p)
	if err != nil {
		return 0, err
	}

	return originalLen, nil
}

// String returns the bytes that were actually captured.
func (b *cappedBuffer) String() string {
	return b.buf.String()
}

// Truncated reports whether any bytes had to be dropped.
func (b *cappedBuffer) Truncated() bool {
	return b.truncated
}

// TotalBytes returns how much data passed through this writer, including any
// bytes that were dropped because of the cap.
func (b *cappedBuffer) TotalBytes() int {
	return b.totalBytes
}
