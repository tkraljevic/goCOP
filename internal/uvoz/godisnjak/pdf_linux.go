//go:build linux

package godisnjak

import (
	"os/exec"
	"runtime"
	"syscall"
)

// pokreniPDF pokreće pdftotext tako da ga sustav ugasi i kad se goCOP
// naglo ugasi, pa rok više nitko ne gleda. Sustav to radi kad završi nit
// koja je proces pokrenula, pa nit ostaje zaključana dok pdftotext radi.
func pokreniPDF(cmd *exec.Cmd) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd.Run()
}
