//go:build linux

package pipery

import "golang.org/x/sys/unix"

func systemMemoryBytes() uint64 {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}

	return info.Totalram * uint64(info.Unit)
}

func normalizeMaxRSSBytes(maxrss int64) uint64 {
	// Linux reports ru_maxrss in kilobytes.
	return uint64(maxrss) * 1024
}
