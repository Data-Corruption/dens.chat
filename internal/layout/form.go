package layout

import "fmt"

// Form is how an instance is deployed (M9): installed, as a service under
// its own account; as a development build's instance, run by the
// developer; as the Flatpak, run by the member in their session; or under
// the Android app's shell (M12). What
// differs by form, from where the instance's paths are to how its data key
// unwraps and what it may do, is chosen at runtime from it, never by build
// tags; the system decides the rest.
type Form int

const (
	// Installed is the service under its own account, which the
	// maintenance commands install, update, restore and uninstall.
	Installed Form = iota
	// Development is a development build's instance, which the developer
	// runs as themselves, with a plaintext data key and data that's theirs
	// to throw away.
	Development
	// Flatpak is the release binary in Dens's Flatpak sandbox (M9.3),
	// which the member runs in their session, from the app's own
	// directories, and Flatpak installs and updates.
	Flatpak
	// Android is Dens built for Android, under the app's shell, as its
	// child process (M12): from the app's files, which the shell names in
	// the environment, with the data key the shell unwraps from the
	// Keystore. The app's updater, or F-Droid, installs and updates it.
	Android
)

func (f Form) String() string {
	switch f {
	case Installed:
		return "installed"
	case Development:
		return "development"
	case Flatpak:
		return "Flatpak"
	case Android:
		return "Android app"
	}
	return fmt.Sprintf("form %d", int(f))
}

// Session reports whether the form runs as its user with no service
// manager: it creates its own directories, instance config and lifecycle
// state, which the maintenance commands write for the installed form.
func (f Form) Session() bool {
	switch f {
	case Development, Flatpak, Android:
		return true
	}
	return false
}
