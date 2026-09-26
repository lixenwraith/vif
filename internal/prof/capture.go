package prof

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync/atomic"
	"time"
)

// labelling is set while a CPU profile runs: timed modules then tag their
// samples kind=<sys|evt|draw|core> module=<name>, which pprof -tagfocus selects
var labelling atomic.Bool

// mutexing is set while a mutex capture samples contention
var mutexing atomic.Bool

// Capture kinds, each a file prefix and extension in the diagnostics directory
var (
	captureCPU   = [2]string{"vif-cpu-", ".pprof"}
	captureHeap  = [2]string{"vif-heap-", ".pprof"}
	captureTrace = [2]string{"vif-trace-", ".out"}
	captureMutex = [2]string{"vif-mutex-", ".pprof"}
)

// StartCPU profiles the next d into dir; done reports the file once it closes
func StartCPU(dir string, d time.Duration, done func(path string, err error)) (string, error) {
	f, path, err := create(dir, captureCPU)
	if err != nil {
		return "", err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		discard(f, path)
		return "", err
	}
	labelling.Store(true)
	time.AfterFunc(d, func() {
		labelling.Store(false)
		pprof.StopCPUProfile()
		done(path, f.Close())
	})
	return path, nil
}

// StartTrace records an execution trace of the next d into dir, with a region
// per timed module; done reports the file once it closes
func StartTrace(dir string, d time.Duration, done func(path string, err error)) (string, error) {
	f, path, err := create(dir, captureTrace)
	if err != nil {
		return "", err
	}
	if err := trace.Start(f); err != nil {
		discard(f, path)
		return "", err
	}
	time.AfterFunc(d, func() {
		trace.Stop()
		done(path, f.Close())
	})
	return path, nil
}

// StartMutex samples every lock contention for the next d, then writes the
// mutex profile: time others waited, by the stack that released the lock. Off,
// the runtime skips sampling; the profile accumulates over a process's captures.
func StartMutex(dir string, d time.Duration, done func(path string, err error)) (string, error) {
	if !mutexing.CompareAndSwap(false, true) {
		return "", errors.New("a mutex capture is already running")
	}
	f, path, err := create(dir, captureMutex)
	if err != nil {
		mutexing.Store(false)
		return "", err
	}
	runtime.SetMutexProfileFraction(1)
	time.AfterFunc(d, func() {
		runtime.SetMutexProfileFraction(0)
		err := pprof.Lookup("mutex").WriteTo(f, 0)
		mutexing.Store(false)
		done(path, errors.Join(err, f.Close()))
	})
	return path, nil
}

// WriteHeap collects, then writes the heap profile: live and allocated space by
// call site since the process started. Blocking; call off the world lock.
func WriteHeap(dir string) (string, error) {
	f, path, err := create(dir, captureHeap)
	if err != nil {
		return "", err
	}
	runtime.GC()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		discard(f, path)
		return "", err
	}
	return path, f.Close()
}

func create(dir string, kind [2]string) (*os.File, string, error) {
	if dir == "" {
		return nil, "", errors.New("no diagnostics directory")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, kind[0]+time.Now().Format("060102-150405")+kind[1])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	return f, path, err
}

func discard(f *os.File, path string) {
	_ = f.Close()
	_ = os.Remove(path)
}
