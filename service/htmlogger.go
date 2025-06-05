package service

import (
	"container/ring"
	"sync"
)

// defaultBufferSize is the default capacity of the ring buffer for log entries.
const defaultBufferSize = 25

// RingBufferLogWriter implements io.Writer to store log entries in a fixed-size ring buffer.
// This is useful for displaying recent logs, for example, on an admin page.
// It is safe for concurrent use.
type RingBufferLogWriter struct {
	mu     sync.Mutex
	buffer *ring.Ring // The ring buffer itself.
	size   int        // The capacity of the buffer.
}

// NewRingBufferLogWriter creates a new RingBufferLogWriter with the default buffer size (defaultBufferSize).
func NewRingBufferLogWriter() *RingBufferLogWriter {
	return NewRingBufferLogWriterWithSize(defaultBufferSize)
}

// NewRingBufferLogWriterWithSize creates a new RingBufferLogWriter with the specified size.
// If the provided size is less than or equal to 0, defaultBufferSize is used.
func NewRingBufferLogWriterWithSize(size int) *RingBufferLogWriter {
	if size <= 0 {
		size = defaultBufferSize
	}
	return &RingBufferLogWriter{
		buffer: ring.New(size),
		size:   size,
	}
}

// Write implements the io.Writer interface. It writes a log entry to the ring buffer.
// The input byte slice p is expected to be a single log entry (e.g., one line of JSON from slog).
// A copy of p is stored to prevent issues if the logger reuses the buffer.
func (w *RingBufferLogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Make a copy of the byte slice, as p might be reused by the logger.
	entry := make([]byte, len(p))
	copy(entry, p)

	w.buffer.Value = string(entry) // Store the log entry as a string.
	w.buffer = w.buffer.Next()     // Advance the ring buffer pointer to the next slot.

	return len(p), nil
}

// GetLogs retrieves all log entries currently stored in the buffer.
// Entries are returned in chronological order (oldest to newest).
// It filters out any uninitialized (nil) slots in the ring buffer.
func (w *RingBufferLogWriter) GetLogs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	// The ring.Do method iterates from r.Next() up to r.Value.
	// Since w.buffer points to the *next slot to be written* (which is effectively the oldest
	// entry if the buffer has wrapped around, or the starting point if not yet full),
	// this iteration order correctly retrieves entries from oldest to newest.
	finalLogs := make([]string, 0, w.size)
	w.buffer.Do(func(val interface{}) {
		if val != nil { // Filter out uninitialized (nil) slots in the ring.
			finalLogs = append(finalLogs, val.(string))
		}
	})
	return finalLogs
}
