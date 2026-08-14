//go:build windows

package processes

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// newJobObject is a seam for tests to force job-creation failures.
var newJobObject = func() (windows.Handle, error) {
	return windows.CreateJobObject(nil, nil)
}

// jobConfinement confines a process tree with a Windows Job Object. The
// child is started CREATE_SUSPENDED and assigned to the job before its main
// thread ever runs, so there is no window in which it (or a grandchild it
// spawns immediately) can escape the job.
type jobConfinement struct {
	mu      sync.Mutex
	job     windows.Handle
	process windows.Handle
	closed  bool
}

// sweepStaleProfiles has nothing to sweep on Windows: confinement is a job
// object, not files on disk. It exists so NewSupervisor's call compiles on
// every platform.
func sweepStaleProfiles() {}

func applyConfinement(cmd *exec.Cmd, spec Spec) (Confinement, error) {
	mode := spec.Confine.Mode
	switch mode {
	case ConfineNone:
		return nil, nil
	case ConfineAgent, ConfineReap:
	default:
		return nil, fmt.Errorf("processes: unknown confinement mode %q: %w", mode, ErrConfinementUnsupported)
	}

	job, err := newJobObject()
	if err != nil {
		if mode == ConfineReap {
			slog.Warn("job object creation failed, starting process unconfined", "error", err)
			return nil, nil
		}
		return nil, fmt.Errorf("create job object: %w", err)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if mode == ConfineAgent {
		policy := spec.Confine.resolved()
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
			windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
			windows.JOB_OBJECT_LIMIT_JOB_MEMORY |
			windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION
		info.BasicLimitInformation.ActiveProcessLimit = policy.MaxProcesses
		info.JobMemoryLimit = uintptr(policy.MaxMemoryBytes)
	} else {
		// ConfineReap ("danger-full-access"): guaranteed reaping only, no
		// process or memory cap. WritableRoots is ignored on Windows too - a
		// job object confines processes and memory, not the filesystem.
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	}
	// BREAKAWAY_OK / SILENT_BREAKAWAY_OK are deliberately never set, so
	// CREATE_BREAKAWAY_FROM_JOB from inside the job fails.

	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		_ = windows.CloseHandle(job)
		if mode == ConfineReap {
			slog.Warn("job object limit configuration failed, starting process unconfined", "error", err)
			return nil, nil
		}
		return nil, fmt.Errorf("configure job object limits: %w", err)
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Start suspended: the process joins the job (Attach, below) before its
	// main thread runs, closing the race where a child that spawns
	// grandchildren immediately in main could win against job assignment.
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED

	return &jobConfinement{job: job}, nil
}

// probeConfinement creates a job object with the same limits applyConfinement
// would use for ConfineAgent, then closes it immediately. No process is ever
// assigned to it, so closing it does not kill anything; the point is only to
// verify job-object creation and limit configuration succeed on this host.
func probeConfinement() error {
	job, err := newJobObject()
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	defer windows.CloseHandle(job)

	policy := ConfinementPolicy{}.resolved()
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
		windows.JOB_OBJECT_LIMIT_JOB_MEMORY |
		windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION
	info.BasicLimitInformation.ActiveProcessLimit = policy.MaxProcesses
	info.JobMemoryLimit = uintptr(policy.MaxMemoryBytes)

	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("configure job object limits: %w", err)
	}
	return nil
}

func (j *jobConfinement) Attach(cmd *exec.Cmd) error {
	pid := uint32(cmd.Process.Pid)
	// PID reuse is not a concern here: os/exec holds the child's handle open
	// until Wait, so this PID cannot have been recycled yet.
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, pid)
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}

	if err := windows.AssignProcessToJobObject(j.job, procHandle); err != nil {
		_ = windows.CloseHandle(procHandle)
		return fmt.Errorf("assign process %d to job: %w", pid, err)
	}

	// resumeMainThread (the documented path) needs a system-wide
	// TH32CS_SNAPTHREAD snapshot plus a syscall-per-thread walk over every
	// thread on the machine to find this process's one thread; measured at
	// tens of milliseconds and scaling with total system thread count, not
	// with our own child. resumeProcess (ntdll!NtResumeProcess) resumes
	// every thread of a process by handle with no enumeration at all; it is
	// undocumented but stable since Windows XP and used by tools like
	// Process Explorer. Try it first and fall back to the documented
	// Toolhelp path on any failure, so resumeMainThread stays the safety
	// net this depends on for correctness.
	if err := resumeProcess(procHandle); err != nil {
		if err := resumeMainThread(pid); err != nil {
			_ = windows.CloseHandle(procHandle)
			return fmt.Errorf("resume process %d: %w", pid, err)
		}
	}

	j.mu.Lock()
	j.process = procHandle
	j.mu.Unlock()
	return nil
}

// procNtResumeProcess resolves ntdll!NtResumeProcess lazily; it is
// undocumented and not exposed by golang.org/x/sys/windows.
var procNtResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// resumeProcess is a seam so tests can force the NtResumeProcess fast path
// to fail and exercise the resumeMainThread fallback.
var resumeProcess = ntResumeProcess

// ntResumeProcess resumes every thread of procHandle in a single call via
// the undocumented ntdll!NtResumeProcess, avoiding the system-wide thread
// enumeration resumeMainThread needs. See the comment in Attach for why
// falling back to resumeMainThread on any failure here is required, not
// optional.
func ntResumeProcess(procHandle windows.Handle) error {
	r0, _, _ := procNtResumeProcess.Call(uintptr(procHandle))
	if status := windows.NTStatus(r0); status != windows.STATUS_SUCCESS {
		return fmt.Errorf("NtResumeProcess: %w", status)
	}
	return nil
}

// resumeMainThread finds and resumes the single thread of a process started
// with CREATE_SUSPENDED. CreateToolhelp32Snapshot documents ERROR_BAD_LENGTH
// as a transient condition (the process/thread list changed between sizing
// and snapshotting), so it is retried a few times.
func resumeMainThread(pid uint32) error {
	var snapshot windows.Handle
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err = windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_BAD_LENGTH) {
			return fmt.Errorf("snapshot threads: %w", err)
		}
	}
	if err != nil {
		return fmt.Errorf("snapshot threads after retries: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("enumerate threads: %w", err)
	}
	var tid uint32
	for {
		if entry.OwnerProcessID == pid {
			tid = entry.ThreadID
			break
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			break
		}
	}
	if tid == 0 {
		return fmt.Errorf("no thread found for pid %d", pid)
	}

	threadHandle, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, tid)
	if err != nil {
		return fmt.Errorf("open thread %d: %w", tid, err)
	}
	defer windows.CloseHandle(threadHandle)

	if _, err := windows.ResumeThread(threadHandle); err != nil {
		return fmt.Errorf("resume thread %d: %w", tid, err)
	}
	return nil
}

func (j *jobConfinement) Kill() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	return windows.TerminateJobObject(j.job, 1)
}

func (j *jobConfinement) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	var err error
	if j.process != 0 {
		if e := windows.CloseHandle(j.process); e != nil {
			err = e
		}
		j.process = 0
	}
	// Closing the last handle to the job is what fires KILL_ON_JOB_CLOSE,
	// reaping anything still inside it.
	if j.job != 0 {
		if e := windows.CloseHandle(j.job); e != nil && err == nil {
			err = e
		}
		j.job = 0
	}
	return err
}
