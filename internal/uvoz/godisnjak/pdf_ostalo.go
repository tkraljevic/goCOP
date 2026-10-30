//go:build !linux

package godisnjak

import "os/exec"

// pokreniPDF pokreće pdftotext. Na ovom sustavu nema načina da ga sustav
// ugasi s goCOP-om, pa vrijedi samo rok (rokPDF).
func pokreniPDF(cmd *exec.Cmd) error { return cmd.Run() }
