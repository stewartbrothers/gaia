//go:build !unix

package chain

import (
	"os"
	"syscall"
)

// procAttrNewSession is the non-Unix fallback. SysProcAttr's process-
// group fields are Unix-specific; on other platforms the default
// (no group manipulation) is used. The trade-off is that fail_fast
// cancellation may leave orphaned subprocesses on Windows — gaia's
// supported deploy target is Linux/macOS so this is documented but
// not load-bearing today.
func procAttrNewSession() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// killProcessGroup falls back to killing only the immediate child
// process. Same caveat as procAttrNewSession.
func killProcessGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
