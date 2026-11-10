//go:build !unix && !windows

package lsp

import "os/exec"

func procAttr(*exec.Cmd) {}

// procTree: no process groups here; a kill reaches only the server itself.
type procTree struct{}

func newProcTree(*exec.Cmd) (*procTree, error) { return nil, nil }
func (*procTree) kill()                        {}
func (*procTree) close()                       {}
