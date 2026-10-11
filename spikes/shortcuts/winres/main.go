// Command winres writes the COFF object that carries a manifest into a Go
// build for Windows, as the resource the linker places in the .exe, and with
// -check prints the manifest a built .exe carries.
//
//	go run ./shortcuts/winres -out shortcuts/rsrc_windows_amd64.syso -arch amd64
//	go run ./shortcuts/winres -check out.exe
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
)

// The manifest from Microsoft's page on the console allocation policy:
// started outside a console, the program gets none (Windows 11 24H2 and
// later), and started in one, it runs as any console program does.
const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <application>
    <windowsSettings>
      <consoleAllocationPolicy xmlns="http://schemas.microsoft.com/SMI/2024/WindowsSettings">detached</consoleAllocationPolicy>
    </windowsSettings>
  </application>
</assembly>
`

const (
	rtManifest = 24 // RT_MANIFEST
	manifestID = 1  // CREATEPROCESS_MANIFEST_RESOURCE_ID
	langEnUS   = 0x0409
	subdir     = 0x80000000

	scnInitializedData = 0x00000040
	scnMemRead         = 0x40000000
	symClassStatic     = 3
)

var machines = map[string]struct {
	machine, addr32nb uint16
}{
	"amd64": {pe.IMAGE_FILE_MACHINE_AMD64, 0x0003}, // IMAGE_REL_AMD64_ADDR32NB
	"arm64": {pe.IMAGE_FILE_MACHINE_ARM64, 0x0002}, // IMAGE_REL_ARM64_ADDR32NB
}

func main() {
	out := flag.String("out", "", "write the object here")
	arch := flag.String("arch", "amd64", "amd64 or arm64")
	check := flag.String("check", "", "print the manifest this .exe carries")
	flag.Parse()
	var err error
	switch {
	case *check != "":
		err = printManifest(*check)
	case *out != "":
		m, ok := machines[*arch]
		if !ok {
			err = fmt.Errorf("unknown arch %q", *arch)
			break
		}
		err = os.WriteFile(*out, object(m.machine, m.addr32nb, []byte(manifest)), 0o644)
	default:
		err = errors.New("pass -out or -check")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
}

// object lays out one .rsrc section: the resource tree, type RT_MANIFEST,
// ID 1, en-US, down to a data entry whose address is section-relative until
// the linker relocates it, then the manifest.
func object(machine, addr32nb uint16, data []byte) []byte {
	le := binary.LittleEndian
	var rsrc bytes.Buffer
	dir := func(id, offset uint32) {
		var d [24]byte
		le.PutUint16(d[14:], 1) // one ID entry, no named ones
		le.PutUint32(d[16:], id)
		le.PutUint32(d[20:], offset)
		rsrc.Write(d[:])
	}
	dir(rtManifest, subdir|24)
	dir(manifestID, subdir|48)
	dir(langEnUS, 72)
	const dataEntry, dataStart = 72, 88
	var e [16]byte
	le.PutUint32(e[0:], dataStart) // the addend the relocation adds the section's address to
	le.PutUint32(e[4:], uint32(len(data)))
	rsrc.Write(e[:])
	rsrc.Write(data)
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
	return f.Bytes()
}

// printManifest walks a built .exe's resource tree to its manifest.
func printManifest(path string) error {
	f, err := pe.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sect := f.Section(".rsrc")
	if sect == nil {
		return errors.New("no .rsrc section: the manifest didn't reach the build")
	}
	b, err := sect.Data()
	if err != nil {
		return err
	}
	le := binary.LittleEndian
	entry := func(dirOff uint32, id uint32) (uint32, error) {
		if int(dirOff)+16 > len(b) {
			return 0, errors.New("resource directory out of range")
		}
		n := uint32(le.Uint16(b[dirOff+12:])) + uint32(le.Uint16(b[dirOff+14:]))
		for i := uint32(0); i < n; i++ {
			at := dirOff + 16 + 8*i
			if int(at)+8 > len(b) {
				break
			}
			if id == 0 || le.Uint32(b[at:]) == id {
				return le.Uint32(b[at+4:]), nil
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
		return err
	}
	if int(off)+16 > len(b) {
		return errors.New("data entry out of range")
	}
	rva, size := le.Uint32(b[off:]), le.Uint32(b[off+4:])
	start := rva - sect.VirtualAddress
	if uint64(start)+uint64(size) > uint64(len(b)) {
		return fmt.Errorf("manifest at RVA %#x, size %d, is outside .rsrc", rva, size)
	}
	fmt.Printf("%s carries a manifest at RVA %#x (%d bytes):\n%s", path, rva, size, b[start:start+size])
	return nil
}
