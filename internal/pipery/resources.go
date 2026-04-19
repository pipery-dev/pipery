package pipery

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type resourceSnapshot struct {
	SystemCPUCores   int
	SystemMemory     uint64
	ProcessUserCPU   int64
	ProcessSystemCPU int64
	ProcessMaxRSS    uint64
}

var (
	systemResourcesOnce sync.Once
	systemResources     resourceSnapshot
)

func cachedSystemResources() resourceSnapshot {
	systemResourcesOnce.Do(func() {
		systemResources = resourceSnapshot{
			SystemCPUCores: runtime.NumCPU(),
			SystemMemory:   systemMemoryBytes(),
		}
	})

	return systemResources
}

func builtinResourceSnapshot() resourceSnapshot {
	snapshot := cachedSystemResources()

	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return snapshot
	}

	snapshot.ProcessUserCPU = durationToMillis(time.Duration(usage.Utime.Sec)*time.Second + time.Duration(usage.Utime.Usec)*time.Microsecond)
	snapshot.ProcessSystemCPU = durationToMillis(time.Duration(usage.Stime.Sec)*time.Second + time.Duration(usage.Stime.Usec)*time.Microsecond)
	snapshot.ProcessMaxRSS = normalizeMaxRSSBytes(usage.Maxrss)
	return snapshot
}

func externalResourceSnapshot(state *os.ProcessState) resourceSnapshot {
	snapshot := cachedSystemResources()
	if state == nil {
		return snapshot
	}

	snapshot.ProcessUserCPU = durationToMillis(state.UserTime())
	snapshot.ProcessSystemCPU = durationToMillis(state.SystemTime())

	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage == nil {
		return snapshot
	}

	snapshot.ProcessMaxRSS = normalizeMaxRSSBytes(usage.Maxrss)
	return snapshot
}

func durationToMillis(value time.Duration) int64 {
	return value.Milliseconds()
}

func applyResourceSnapshot(entry *logEntry, snapshot resourceSnapshot) {
	entry.SystemCPUCores = snapshot.SystemCPUCores
	entry.SystemMemory = snapshot.SystemMemory
	entry.ProcessUserCPU = snapshot.ProcessUserCPU
	entry.ProcessSystemCPU = snapshot.ProcessSystemCPU
	entry.ProcessMaxRSS = snapshot.ProcessMaxRSS
}

func mergeResourceSnapshots(base resourceSnapshot, entry logEntry) resourceSnapshot {
	if entry.SystemCPUCores != 0 {
		base.SystemCPUCores = entry.SystemCPUCores
	}
	if entry.SystemMemory != 0 {
		base.SystemMemory = entry.SystemMemory
	}
	if entry.ProcessUserCPU != 0 {
		base.ProcessUserCPU = entry.ProcessUserCPU
	}
	if entry.ProcessSystemCPU != 0 {
		base.ProcessSystemCPU = entry.ProcessSystemCPU
	}
	if entry.ProcessMaxRSS != 0 {
		base.ProcessMaxRSS = entry.ProcessMaxRSS
	}

	return base
}
