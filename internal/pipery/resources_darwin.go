//go:build darwin

package pipery

import "golang.org/x/sys/unix"

func systemMemoryBytes() uint64 {
	value, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}

	return value
}

func normalizeMaxRSSBytes(maxrss int64) uint64 {
	// macOS reports ru_maxrss in bytes.
	return uint64(maxrss)
}
