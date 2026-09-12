//go:build windows

package processes

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows Job Objects provide process-tree lifetime and resource limits. They
// do not provide a filesystem sandbox; the docs state that limitation plainly.
// A required request still fails closed if the job cannot be created or the
// process cannot be assigned to it.
func preparePlatformContainment(cmd *exec.Cmd, spec ContainmentSpec) error {
	if !spec.required() {
		return nil
	}
	if err := spec.validate(); err != nil {
		return err
	}
	if spec.Network != NetworkUnrestricted {
		return fmt.Errorf("network policy %q is not supported by the Windows Job Object backend", spec.Network)
	}
	// Keep the process suspended until it is inside the Job Object. This closes
	// the startup race where a child could execute or spawn before assignment.
	// configureProcessTree preserves this flag when it adds the process group.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	return nil
}

func attachPlatformContainment(cmd *exec.Cmd, spec ContainmentSpec) (func(), error) {
	if !spec.required() {
		return nil, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create Job Object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
	info.BasicLimitInformation.ActiveProcessLimit = 128
	// A bounded per-run memory ceiling prevents a runaway tool from exhausting
	// the operator's machine; it is intentionally generous for Go/npm builds.
	info.JobMemoryLimit = 4 * 1024 * 1024 * 1024
	info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure Job Object: %w", err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("open child process for Job Object: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign child process to Job Object: %w", err)
	}
	if err := resumeSuspendedProcess(uint32(cmd.Process.Pid)); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("resume child process after Job Object assignment: %w", err)
	}
	return func() { _ = windows.CloseHandle(job) }, nil
}

func resumeSuspendedProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == pid {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			_, resumeErr := windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			return resumeErr
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			return fmt.Errorf("primary thread not found: %w", err)
		}
	}
}

func cleanupPreparedPlatformContainment(_ *exec.Cmd)                      {}
func preparedNetworkObservations(_ *exec.Cmd) func() []NetworkObservation { return nil }

// Keep syscall referenced on old Go/windows combinations where exec.Cmd's
// SysProcAttr uses syscall.SysProcAttr and the compiler otherwise drops the
// platform import during cross-build checks.
var _ = syscall.CREATE_NEW_PROCESS_GROUP
