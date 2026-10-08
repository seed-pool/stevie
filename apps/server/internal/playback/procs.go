package playback

import (
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcRegistry tracks live ffmpeg children so they can be force-killed on
// client abort or process shutdown.
type ProcRegistry struct {
	mu   sync.Mutex
	cmds map[int]*exec.Cmd
}

// DefaultProcs is the process-wide registry used by remux/subtitle helpers.
var DefaultProcs = NewProcRegistry()

func NewProcRegistry() *ProcRegistry {
	return &ProcRegistry{cmds: make(map[int]*exec.Cmd)}
}

func (r *ProcRegistry) Add(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	r.mu.Lock()
	r.cmds[cmd.Process.Pid] = cmd
	r.mu.Unlock()
}

func (r *ProcRegistry) Remove(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	r.mu.Lock()
	delete(r.cmds, cmd.Process.Pid)
	r.mu.Unlock()
}

func (r *ProcRegistry) Kill(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	r.mu.Lock()
	_, tracked := r.cmds[pid]
	if tracked {
		delete(r.cmds, pid)
	}
	r.mu.Unlock()

	killPID(pid)

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		slog.Warn("ffmpeg wait timed out after kill", "pid", pid)
	}
}

// KillAll terminates every tracked ffmpeg process. Safe during shutdown.
func (r *ProcRegistry) KillAll() {
	r.mu.Lock()
	cmds := make([]*exec.Cmd, 0, len(r.cmds))
	for _, c := range r.cmds {
		cmds = append(cmds, c)
	}
	r.mu.Unlock()
	for _, c := range cmds {
		r.Kill(c)
	}
	killStrayFFmpeg()
}

func (r *ProcRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cmds)
}

func killPID(pid int) {
	if pid <= 1 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func killStrayFFmpeg() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		comm, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) != "ffmpeg" {
			continue
		}
		slog.Warn("killing stray ffmpeg", "pid", pid)
		killPID(pid)
	}
}
