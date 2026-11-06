package autostart

import (
	"github.com/patrache/masil/internal/config"
	"golang.org/x/sys/windows/registry"
)

// runKey is the real HKCU Run key; tests swap it.
var runKey runKeyStore = registryRunKey{}

// Install registers `"<exe>" daemon run` as the HKCU Run value masil-daemon (contract 9.2).
// The console window is hidden by the daemon's own self-respawn, not here.
func Install(exe string, p config.Paths) error { return runKeyInstall(runKey, exe) }

// Uninstall deletes the HKCU Run value.
func Uninstall() error { return runKeyUninstall(runKey) }

// Query reports the HKCU Run value registration.
func Query() (Status, error) { return runKeyQuery(runKey) }

// registryRunKey is runKeyStore over HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
type registryRunKey struct{}

func (registryRunKey) Get(name string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return v, err
}

func (registryRunKey) Set(name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func (registryRunKey) Delete(name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(name)
}
