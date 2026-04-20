//go:build !linux && !darwin

package pipery

func systemMemoryBytes() uint64 {
	return 0
}

func normalizeMaxRSSBytes(maxrss int64) uint64 {
	if maxrss < 0 {
		return 0
	}
	return uint64(maxrss)
}
