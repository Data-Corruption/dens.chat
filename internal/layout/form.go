package layout

import "fmt"

// Form is how an instance is deployed (M9): installed, as a service under
// its own account, or as a development build's instance, run by the
// developer. What differs by form, from where the instance's paths are to
// how its data key unwraps and what it may do, is chosen at runtime from
// it, never by build tags; the system decides the rest.
type Form int

const (
	// Installed is the service under its own account, which the
	// maintenance commands install, update, restore and uninstall.
	Installed Form = iota
	// Development is a development build's instance, which the developer
	// runs as themselves, with a plaintext data key and data that's theirs
	// to throw away.
	Development
)

func (f Form) String() string {
	switch f {
	case Installed:
		return "installed"
	case Development:
		return "development"
	}
	return fmt.Sprintf("form %d", int(f))
}

// Session reports whether the form runs as its user with no service
// manager: it creates its own directories, instance config and lifecycle
// state, which the maintenance commands write for the installed form.
func (f Form) Session() bool {
	switch f {
	case Development:
		return true
	}
	return false
}
