module github.com/Data-Corruption/dens.chat/spikes

go 1.26.7

require (
	github.com/Data-Corruption/dens.chat v0.0.0
	github.com/godbus/dbus/v5 v5.2.2
	golang.org/x/sys v0.48.0
)

// The spikes run Dens's own seams where they exist, from this tree.
replace github.com/Data-Corruption/dens.chat => ../
