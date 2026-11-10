//go:build !windows

package lsp

import "os/exec"

func hideWindow(*exec.Cmd) {}
