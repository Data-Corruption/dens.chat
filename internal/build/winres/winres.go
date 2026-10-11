// Package winres writes the resource object that puts dens.exe's manifest
// into Windows builds. Go embeds no manifest itself, but its linker places
// the .rsrc section of an object file (a .syso) beside the main package in
// the binary, so cmd/ holds one for each Windows architecture, generated
// here and committed, and a test fails when they're stale.
package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
)

// Manifest asks Windows (11 24H2 and later) to give dens.exe no console of
// its own when nothing started it from one, as Task Scheduler starts the
// shortcut helper at login, so the helper opens no window, while in a
// shell it runs as any console program does.
const Manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <application>
    <windowsSettings>
      <consoleAllocationPolicy xmlns="http://schemas.microsoft.com/SMI/2024/WindowsSettings">detached</consoleAllocationPolicy>
    </windowsSettings>
  </application>
</assembly>
`

// Arches are the Windows architectures releases build.
var Arches = []string{"amd64", "arm64"}

// FileName is the object's name in cmd/ for arch, which only that
// architecture's Windows builds link.
func FileName(arch string) string { return "rsrc_windows_" + arch + ".syso" }

const (
	rtManifest = 24 // RT_MANIFEST
	manifestID = 1  // CREATEPROCESS_MANIFEST_RESOURCE_ID
	langEnUS   = 0x0409
	subdir     = 0x80000000

	scnInitializedData = 0x00000040
	scnMemRead         = 0x40000000
	symClassStatic     = 3
)

// Object is the COFF object for arch: one .rsrc section holding the
// resource tree (type RT_MANIFEST, ID 1, en-US) down to a data entry whose
// address the linker relocates, then the manifest.
func Object(arch string) ([]byte, error) {
	var machine, addr32nb uint16
	switch arch {
	case "amd64":
		machine, addr32nb = pe.IMAGE_FILE_MACHINE_AMD64, 0x0003 // IMAGE_REL_AMD64_ADDR32NB
	case "arm64":
		machine, addr32nb = pe.IMAGE_FILE_MACHINE_ARM64, 0x0002 // IMAGE_REL_ARM64_ADDR32NB
	default:
		return nil, fmt.Errorf("no resource object for %s", arch)
	}
	le := binary.LittleEndian
	var rsrc bytes.Buffer
	dir := func(id, offset uint32) {
		var d [24]byte
		le.PutUint16(d[14:], 1) // one entry by ID, none by name
		le.PutUint32(d[16:], id)
		le.PutUint32(d[20:], offset)
		rsrc.Write(d[:])
	}
	dir(rtManifest, subdir|24)
	dir(manifestID, subdir|48)
	dir(langEnUS, 72)
	const dataEntry, dataStart = 72, 88
	var e [16]byte
	le.PutUint32(e[0:], dataStart) // the linker adds the section's address
	le.PutUint32(e[4:], uint32(len(Manifest)))
	rsrc.Write(e[:])
	rsrc.WriteString(Manifest)
	for rsrc.Len()%8 != 0 {
		rsrc.WriteByte(0)
	}

	const headers = 20 + 40
	relocs := headers + rsrc.Len()
	symbols := relocs + 10
	var f bytes.Buffer
	var h [20]byte
	le.PutUint16(h[0:], machine)
	le.PutUint16(h[2:], 1) // sections
	le.PutUint32(h[8:], uint32(symbols))
	le.PutUint32(h[12:], 1) // symbols
	f.Write(h[:])
	var s [40]byte
	copy(s[0:8], ".rsrc")
	le.PutUint32(s[16:], uint32(rsrc.Len()))
	le.PutUint32(s[20:], headers)
	le.PutUint32(s[24:], uint32(relocs))
	le.PutUint16(s[32:], 1) // relocations
	le.PutUint32(s[36:], scnInitializedData|scnMemRead)
	f.Write(s[:])
	f.Write(rsrc.Bytes())
	var r [10]byte
	le.PutUint32(r[0:], dataEntry)
	le.PutUint32(r[4:], 0) // the section's symbol
	le.PutUint16(r[8:], addr32nb)
	f.Write(r[:])
	var sym [18]byte
	copy(sym[0:8], ".rsrc")
	le.PutUint16(sym[12:], 1) // section number
	sym[16] = symClassStatic
	f.Write(sym[:])
	f.Write([]byte{4, 0, 0, 0}) // an empty string table
	return f.Bytes(), nil
}

// ManifestOf returns the manifest a resource section carries, walking its
// tree as Windows does; base is the section's address, against which the
// data entry's address is relative.
func ManifestOf(rsrc []byte, base uint32) ([]byte, error) {
	le := binary.LittleEndian
	entry := func(dir uint32, id uint32) (uint32, error) {
		if uint64(dir)+16 > uint64(len(rsrc)) {
			return 0, fmt.Errorf("resource directory at %d is out of range", dir)
		}
		n := uint32(le.Uint16(rsrc[dir+12:])) + uint32(le.Uint16(rsrc[dir+14:]))
		for i := range n {
			at := dir + 16 + 8*i
			if uint64(at)+8 > uint64(len(rsrc)) {
				break
			}
			if id == 0 || le.Uint32(rsrc[at:]) == id {
				return le.Uint32(rsrc[at+4:]), nil
			}
		}
		return 0, fmt.Errorf("no resource entry %d", id)
	}
	off, err := entry(0, rtManifest)
	if err == nil {
		off, err = entry(off&^subdir, 0)
	}
	if err == nil {
		off, err = entry(off&^subdir, 0)
	}
	if err != nil {
		return nil, err
	}
	if uint64(off)+16 > uint64(len(rsrc)) {
		return nil, fmt.Errorf("data entry at %d is out of range", off)
	}
	rva, size := le.Uint32(rsrc[off:]), le.Uint32(rsrc[off+4:])
	start := uint64(rva) - uint64(base)
	if rva < base || start+uint64(size) > uint64(len(rsrc)) {
		return nil, fmt.Errorf("manifest at %#x, %d bytes, is outside the section", rva, size)
	}
	return rsrc[start : start+uint64(size)], nil
}
