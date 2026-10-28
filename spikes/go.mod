module github.com/Data-Corruption/dens.chat/spikes

go 1.26.7

require (
	github.com/Data-Corruption/dens.chat v0.0.0
	github.com/wlynxg/anet v0.0.5
	golang.org/x/sys v0.48.0
)

require (
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/mod v0.31.0 // indirect
)

// The spikes run Dens's own seams where they exist, from this tree.
replace github.com/Data-Corruption/dens.chat => ../
