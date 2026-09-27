package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func (l *Layout) resolve() error {
	title := strings.ToUpper(l.App[:1]) + l.App[1:]
	if l.Dev {
		local := os.Getenv("LOCALAPPDATA")
		if !filepath.IsAbs(local) {
			var err error
			if local, err = knownFolder(windows.FOLDERID_LocalAppData); err != nil {
				return err
			}
		}
		l.Root = filepath.Join(local, title+"-dev", l.Instance)
		l.derive()
		l.HostKey = filepath.Join(l.Control, "datakey.dev")
		l.ControlEndpoint = fmt.Sprintf(`\\.\pipe\%s-dev-%s-control`, l.App, l.Instance)
		return nil
	}
	programData, err := knownFolder(windows.FOLDERID_ProgramData)
	if err != nil {
		return err
	}
	programFiles, err := knownFolder(windows.FOLDERID_ProgramFiles)
	if err != nil {
		return err
	}
	l.Root = filepath.Join(programData, title, l.Instance)
	l.derive()
	l.HostKey = filepath.Join(l.Control, "datakey.dpapi")
	l.ControlEndpoint = fmt.Sprintf(`\\.\pipe\%s-%s-control`, l.App, l.Instance)
	l.BinaryDir = filepath.Join(programFiles, title)
	l.Binary = filepath.Join(l.BinaryDir, l.App+".exe")
	l.Cosign = filepath.Join(l.BinaryDir, "cosign.exe")
	l.ServiceName = l.App + "-" + l.Instance
	l.Account = `NT SERVICE\` + l.ServiceName
	return nil
}

func knownFolder(id *windows.KNOWNFOLDERID) (string, error) {
	path, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		return "", fmt.Errorf("resolve known folder: %w", err)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("known folder path is not absolute: %q", path)
	}
	return path, nil
}
