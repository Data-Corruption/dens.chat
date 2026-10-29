package host

import "errors"

// MakeShortcut is Windows' (M12.3): Linux names apps to the desktop with
// .desktop files, which the installer writes.
func MakeShortcut(path, target, args, description, appID string) error { return errors.ErrUnsupported }
