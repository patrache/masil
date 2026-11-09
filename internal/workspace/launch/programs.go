package launch

import "path/filepath"

// Program is one choice in the "new terminal" list. The last entry has a nil
// Argv and stands for "type a command" (the UI labels it from the catalog).
type Program struct {
	Name string
	Argv []string
}

// Custom reports the "type a command" entry.
func (p Program) Custom() bool { return p.Argv == nil }

// Programs lists the login shell ($SHELL, "sh" when empty), then claude and
// nvim when lookPath finds them, then the custom entry.
func Programs(shell string, lookPath func(string) (string, error)) []Program {
	if shell == "" {
		shell = "sh"
	}
	out := []Program{{Name: filepath.Base(filepath.ToSlash(shell)), Argv: []string{shell}}}
	for _, name := range []string{"claude", "nvim"} {
		if lookPath != nil {
			if _, err := lookPath(name); err == nil {
				out = append(out, Program{Name: name, Argv: []string{name}})
			}
		}
	}
	return append(out, Program{})
}
