//go:build unix

package chain

import "syscall"

// procAttrNewSession returns the SysProcAttr that puts the child into
// its own process group (Setpgid). Required so killProcessGroup can
// terminate the entire tree on context cancellation rather than just
// the immediate child shell — see the comment in execShell for the
// fail_fast cancellation bug this guards against.
func procAttrNewSession() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the entire process group whose
// leader is `pid`. Negative PID is the kill(2) convention for
// "signal every process in this group." The child must have been
// started with Setpgid: true (see procAttrNewSession) for this to
// hit the right group.
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
