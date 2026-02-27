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
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("failed to set subreaper: %w", err)
	}

	go r.handleSigchld()
	return nil
}

func (r *reaper) handleSigchld() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGCHLD)

	for {
		<-sigChan
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
			return
		}
		if pid <= 0 {
			return
		}

		r.updateContainerState(pid)
	}
}

func (r *reaper) RegisterContainer(pid int, containerRoot string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pidMap[pid] = containerRoot
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

	statePath := filepath.Join(containerRoot, stateFilename)
	data, err := ioutil.ReadFile(statePath)
	if err != nil {
		return
	}

	var currentState State
	if err := json.Unmarshal(data, &currentState); err != nil {
		return
	}

	currentState.Status = Stopped
	data, err = json.MarshalIndent(currentState, "", "  ")
	if err != nil {
		return
	}

	if err := ioutil.WriteFile(statePath, data, 0644); err != nil {
		return
	}
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
