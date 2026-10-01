package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"

	trapssafe "github.com/Data-Corruption/dens.chat/spikes/ffmpeg/traps/safe"
	trapsunsafe "github.com/Data-Corruption/dens.chat/spikes/ffmpeg/traps/unsafe"
)

// trapModule is the trap module's exports, as both translations give them.
type trapModule interface {
	X_initialize()
	Xload(int32) int32
	Xstore(int32)
	Xfill_past_end(int32) int32
	Xcopy_past_end(int32) int32
	Xfresh_page_is_zero() int32
	Xcall_index(int32) int32
	Xcall_wrong_type() int64
	Xunreachable()
	Xdivide(int32, int32) int32
	Xshadow_stack(int32) int32
	Xgo_stack(int32) int32
	Xballoon() int32
	Xspin()
}

// trap runs one of A4's attempts: trap unsafe|safe NAME [ARG]. Its result is
// what the export returned; a trap is a panic, which job's caller reports.
func trap(h *host, args []string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("trap: %v", r)
			if os.Getenv("FFSPIKE_TRACE") != "" {
				os.Stderr.Write(debug.Stack())
			}
		}
	}()
	if len(args) < 2 {
		return fmt.Errorf("usage: trap unsafe|safe NAME [ARG]")
	}
	var m trapModule
	var pages int64
	switch args[0] {
	case "unsafe":
		pages = trapsunsafe.MinPages
	case "safe":
		pages = trapssafe.MinPages
	default:
		return fmt.Errorf("no translation %q", args[0])
	}
	if h.mem.Grow(pages, h.mem.max) < 0 {
		return fmt.Errorf("the memory cap is below the module's minimum")
	}
	if args[0] == "unsafe" {
		m = trapsunsafe.New(h)
	} else {
		m = trapssafe.New(h)
	}
	m.X_initialize()
	arg := int64(0)
	if len(args) > 2 {
		if arg, err = strconv.ParseInt(args[2], 0, 64); err != nil {
			return err
		}
	}
	end := int64(len(h.mem.buf))
	var ret any
	switch args[1] {
	case "load-end":
		ret = m.Xload(int32(uint32(end)))
	case "load-straddle":
		ret = m.Xload(int32(uint32(end - 2)))
	case "load-top":
		ret = m.Xload(-4) // 0xfffffffc, the top of the 32-bit address space
	case "store-end":
		m.Xstore(int32(uint32(end)))
	case "store-straddle":
		m.Xstore(int32(uint32(end - 2)))
	case "fill":
		ret = m.Xfill_past_end(int32(arg))
	case "copy":
		ret = m.Xcopy_past_end(int32(arg))
	case "fill-then-grow":
		// Whether a fill past the end can leave bytes in a page grown later.
		func() {
			defer func() { recover() }()
			m.Xfill_past_end(int32(arg))
		}()
		ret = map[string]int32{"fresh_page_is_zero": m.Xfresh_page_is_zero()}
	case "call-null":
		ret = m.Xcall_index(0)
	case "call-past-table":
		ret = m.Xcall_index(int32(arg))
	case "call-wrong-type":
		ret = m.Xcall_wrong_type()
	case "unreachable":
		m.Xunreachable()
	case "divide-zero":
		ret = m.Xdivide(1, 0)
	case "divide-overflow":
		ret = m.Xdivide(math.MinInt32, -1)
	case "shadow-stack":
		ret = m.Xshadow_stack(int32(arg))
	case "go-stack":
		ret = m.Xgo_stack(int32(arg))
	case "balloon":
		ret = map[string]int32{"mb": m.Xballoon()}
	case "spin":
		m.Xspin()
	default:
		return fmt.Errorf("no attempt %q", args[1])
	}
	h.result, _ = json.Marshal(map[string]any{"returned": ret})
	return nil
}
