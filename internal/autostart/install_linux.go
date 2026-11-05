package autostart

import "github.com/patrache/masil/internal/config"

// Install registers `<exe> daemon run` as the systemd user unit masil-daemon.service (contract 9.2).
func Install(exe string, p config.Paths) error { return systemdInstall(exe, p) }

// Uninstall disables and removes the systemd user unit.
func Uninstall() error { return systemdUninstall() }

// Query reports the systemd user unit registration.
func Query() (Status, error) { return systemdQuery() }
