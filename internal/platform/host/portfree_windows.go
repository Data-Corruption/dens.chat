package host

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modiphlpapi             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = modiphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUdpTable = modiphlpapi.NewProc("GetExtendedUdpTable")
)

// Table classes: TCP_TABLE_OWNER_PID_LISTENER and UDP_TABLE_OWNER_PID.
const (
	tcpTableOwnerPIDListener = 3
	udpTableOwnerPID         = 1
)

// socketRow describes one row layout of the owner-PID socket tables.
type socketRow struct {
	size, port, pid int
}

var socketRows = map[string]map[uint32]socketRow{
	// MIB_TCPROW_OWNER_PID and MIB_TCP6ROW_OWNER_PID.
	"tcp": {windows.AF_INET: {24, 8, 20}, windows.AF_INET6: {56, 20, 52}},
	// MIB_UDPROW_OWNER_PID and MIB_UDP6ROW_OWNER_PID.
	"udp": {windows.AF_INET: {12, 4, 8}, windows.AF_INET6: {28, 20, 24}},
}

// PortFree returns an error if another socket holds port on any address:
// a TCP listener, or a bound UDP socket. network is "tcp" or "udp".
//
// It reads the system's socket tables instead of binding the port: an
// interactive process that binds a non-loopback address makes Windows
// Firewall ask the user to allow it, and leaves a rule behind if they do.
func PortFree(network string, port int) error {
	proc, class := procGetExtendedTcpTable, uint32(tcpTableOwnerPIDListener)
	switch network {
	case "tcp":
	case "udp":
		proc, class = procGetExtendedUdpTable, udpTableOwnerPID
	default:
		return fmt.Errorf("unsupported network %q", network)
	}
	for _, family := range []uint32{windows.AF_INET, windows.AF_INET6} {
		table, err := socketTable(proc, family, class)
		if err != nil {
			return fmt.Errorf("read the %s socket table: %w", network, err)
		}
		row := socketRows[network][family]
		entries := int(binary.LittleEndian.Uint32(table))
		for i := range entries {
			offset := 4 + i*row.size
			if offset+row.size > len(table) {
				return errors.New("socket table is shorter than its entry count")
			}
			r := table[offset : offset+row.size]
			// The port is in network byte order in the low 16 bits.
			if int(r[row.port])<<8|int(r[row.port+1]) == port {
				return fmt.Errorf("%s port %d is in use by process %d", network, port, binary.LittleEndian.Uint32(r[row.pid:]))
			}
		}
	}
	return nil
}

// socketTable calls a GetExtended*Table function until its buffer is large
// enough, since the table can grow between the size query and the read.
func socketTable(proc *windows.LazyProc, family, class uint32) ([]byte, error) {
	size := uint32(4)
	for {
		buf := make([]byte, size)
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0,
			uintptr(family), uintptr(class), 0)
		switch errno := windows.Errno(r); errno {
		case windows.ERROR_SUCCESS:
			return buf, nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			continue
		default:
			return nil, errno
		}
	}
}
