package libcontainer

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type reaper struct {
	notify chan struct{}
	mu     sync.Mutex
	pidMap map[int]string // pid -> container root path
}

var globalReaper *reaper

func newReaper() *reaper {
	return &reaper{
		notify: make(chan struct{}),
		pidMap: make(map[int]string),
	}
}

func (r *reaper) start() error {
	fmt.Fprintf(os.Stderr, "DEBUG REAPER: starting subreaper\n")

	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("failed to set subreaper: %w", err)
	}

	fmt.Fprintf(os.Stderr, "DEBUG REAPER: subreaper set, starting signal handler\n")
	go r.handleSigchld()
	return nil
}

func (r *reaper) handleSigchld() {
	fmt.Fprintf(os.Stderr, "DEBUG REAPER: signal handler started, waiting for SIGCHLD\n")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGCHLD)

	for {
		<-sigChan
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: received SIGCHLD\n")
		r.reap()
	}
}

func (r *reaper) reap() {
	for {
		var wstatus unix.WaitStatus
		var rusage unix.Rusage
		pid, err := unix.Wait4(-1, &wstatus, unix.WNOHANG, &rusage)
		if err != nil {
			if err == unix.ECHILD {
				return
			}
			fmt.Fprintf(os.Stderr, "DEBUG REAPER: wait4 error: %v\n", err)
			return
		}
		if pid <= 0 {
			return
		}

		fmt.Fprintf(os.Stderr, "DEBUG REAPER: reaped child PID=%d, status=%d\n", pid, wstatus.ExitStatus())

		// Check if this is a container process and update state
		r.updateContainerState(pid)
	}
}

func (r *reaper) RegisterContainer(pid int, containerRoot string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pidMap[pid] = containerRoot
	fmt.Fprintf(os.Stderr, "DEBUG REAPER: registered container PID=%d, root=%s\n", pid, containerRoot)
}

func (r *reaper) updateContainerState(pid int) {
	r.mu.Lock()
	containerRoot, exists := r.pidMap[pid]
	if exists {
		delete(r.pidMap, pid)
	}
	r.mu.Unlock()

	if !exists {
		return
	}

	fmt.Fprintf(os.Stderr, "DEBUG REAPER: updating state for container at %s\n", containerRoot)

	statePath := filepath.Join(containerRoot, stateFilename)
	data, err := ioutil.ReadFile(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: failed to read state: %v\n", err)
		return
	}

	var currentState State
	if err := json.Unmarshal(data, &currentState); err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: failed to unmarshal state: %v\n", err)
		return
	}

	currentState.Status = Stopped
	data, err = json.MarshalIndent(currentState, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: failed to marshal state: %v\n", err)
		return
	}

	if err := ioutil.WriteFile(statePath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: failed to write state: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "DEBUG REAPER: state updated to stopped\n")
}

func InitReaper() error {
	if globalReaper != nil {
		return nil
	}
	globalReaper = newReaper()
	return globalReaper.start()
}

func RegisterContainer(pid int, containerRoot string) {
	if globalReaper != nil {
		globalReaper.RegisterContainer(pid, containerRoot)
	}
}
