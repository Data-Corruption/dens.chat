package winres

import (
	"bytes"
	"debug/pe"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the resource objects in cmd/")

// TestCommittedObjects keeps cmd/'s objects current; after changing the
// manifest, run: go test ./internal/build/winres -args -update
func TestCommittedObjects(t *testing.T) {
	for _, arch := range Arches {
		want, err := Object(arch)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("..", "..", "..", "cmd", FileName(arch))
		if *update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v; write it with: go test ./internal/build/winres -args -update", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is stale; rewrite it with: go test ./internal/build/winres -args -update", path)
		}
	}
}

// TestObjectCarriesTheManifest reads each object back as the linker does:
// one .rsrc section, a relocation of the data entry against the section's
// symbol, and the manifest at the end of the tree.
func TestObjectCarriesTheManifest(t *testing.T) {
	for _, arch := range Arches {
		data, err := Object(arch)
		if err != nil {
			t.Fatal(err)
		}
		f, err := pe.NewFile(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		if len(f.Sections) != 1 || f.Sections[0].Name != ".rsrc" {
			t.Fatalf("%s: sections %v", arch, f.Sections)
		}
		sect := f.Sections[0]
		if len(sect.Relocs) != 1 || sect.Relocs[0].VirtualAddress != 72 || sect.Relocs[0].SymbolTableIndex != 0 {
			t.Fatalf("%s: relocations %+v", arch, sect.Relocs)
		}
		if len(f.COFFSymbols) != 1 || f.COFFSymbols[0].SectionNumber != 1 || f.COFFSymbols[0].StorageClass != symClassStatic {
			t.Fatalf("%s: symbols %+v", arch, f.COFFSymbols)
		}
		rsrc, err := sect.Data()
		if err != nil {
			t.Fatal(err)
		}
		// Unrelocated, the data entry's address is the manifest's offset.
		got, err := ManifestOf(rsrc, 0)
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		if string(got) != Manifest {
			t.Fatalf("%s: manifest = %q", arch, got)
		}
	}
	if _, err := Object("386"); err == nil {
		t.Fatal("made an object for an architecture releases don't build")
	}
}
