//go:build linux

package godisnjak

import (
	"os/exec"
	"syscall"
	"testing"
)

// Na Linuxu sustav gasi pdftotext kad se goCOP ugasi.
func TestPdftotextSeGasiSProgramom(t *testing.T) {
	cmd := exec.Command("true")
	if err := pokreniPDF(cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Errorf("pdftotext se ne gasi s programom: %+v", cmd.SysProcAttr)
	}
}
