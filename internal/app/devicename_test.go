package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
)

// The name the member gave the install makes its label on the dens and in
// Relay as it starts; a restore forgets it, as it makes the Relay ID
// afresh, since both name the computer.
func TestARestoreForgetsTheComputer(t *testing.T) {
	devHome(t)
	a := openDev(t, fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000))
	defer a.Close()
	if err := a.SetDeviceName("Bob's desk"); err != nil {
		t.Fatal(err)
	}
	before, err := config.View(a.DB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.newRelay(before, false); err != nil {
		t.Fatal(err)
	}
	if kept, _ := config.View(a.DB); kept.DeviceName != "Bob's desk" || kept.RelayID != before.RelayID || a.Dens.Label() != denclient.ComputerLabel("Bob's desk") {
		t.Fatalf("a start keeps %q and %q, the dens getting %q", kept.DeviceName, kept.RelayID, a.Dens.Label())
	}

	if _, err := a.newRelay(before, true); err != nil {
		t.Fatal(err)
	}
	after, err := config.View(a.DB)
	if err != nil {
		t.Fatal(err)
	}
	if after.DeviceName != "" || after.RelayID == before.RelayID || a.Dens.Label() != denclient.ComputerLabel("") {
		t.Fatalf("a restore keeps %q, the Relay ID %q from %q, the dens getting %q", after.DeviceName, after.RelayID, before.RelayID, a.Dens.Label())
	}
}
