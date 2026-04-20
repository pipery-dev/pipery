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
		// These machine-level facts do not change during the lifetime of psh, so
		// we resolve them once and then reuse them for every log entry.
		systemResources = resourceSnapshot{
			SystemCPUCores: runtime.NumCPU(),
			SystemMemory:   systemMemoryBytes(),
		}
	})

	return systemResources
}

func builtinResourceSnapshot() resourceSnapshot {
	snapshot := cachedSystemResources()

	// Built-ins run inside the psh process, so the lightest portable way to
	// capture resource usage is a single Getrusage syscall after the builtin
	// finishes. We intentionally avoid any background sampling or polling.
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

	// For external commands we reuse the process accounting data that Wait has
	// already made available through ProcessState. That keeps logging overhead to
	// a minimum because there is no extra syscall here in the common case.
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
