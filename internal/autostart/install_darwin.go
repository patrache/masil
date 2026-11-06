package autostart

import "github.com/patrache/masil/internal/config"

// Install registers `<exe> daemon run` as the LaunchAgent com.patrache.masil.daemon (contract 9.2).
func Install(exe string, p config.Paths) error { return launchdInstall(exe, p) }

// Uninstall boots the LaunchAgent out and removes its plist.
func Uninstall() error { return launchdUninstall() }

// Query reports the LaunchAgent registration.
func Query() (Status, error) { return launchdQuery() }
