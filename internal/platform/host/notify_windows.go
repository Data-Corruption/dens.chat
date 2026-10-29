package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The desktop's notifications on Windows are toasts (M12.3), through
// Windows Runtime, which Windows shows only for an app ID that a Start
// menu shortcut carries: the installer makes it. A toast's click opens the
// page's address itself, through the browser, so nothing of Dens needs to
// be running then, and from the Action Center as well as the toast.

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoUninitialize         = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procRoActivateInstance     = combase.NewProc("RoActivateInstance")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")

	iidToastManagerStatics  = windows.GUID{Data1: 0x50AC103F, Data2: 0xD235, Data3: 0x4598, Data4: [8]byte{0xBB, 0xEF, 0x98, 0xFE, 0x4D, 0x1A, 0x3A, 0xD4}}
	iidToastManagerStatics2 = windows.GUID{Data1: 0x7AB93C52, Data2: 0x0E48, Data3: 0x4750, Data4: [8]byte{0xBA, 0x9D, 0x1A, 0x41, 0x13, 0x98, 0x18, 0x47}}
	iidToastFactory         = windows.GUID{Data1: 0x04124B20, Data2: 0x82C6, Data3: 0x4229, Data4: [8]byte{0xB1, 0x09, 0xFD, 0x9E, 0xD4, 0x66, 0x2B, 0x53}}
	iidToastNotification2   = windows.GUID{Data1: 0x9DFB9FD1, Data2: 0x143A, Data3: 0x490E, Data4: [8]byte{0x90, 0xBF, 0xB9, 0xFB, 0xA7, 0x13, 0x2D, 0xE7}}
	iidXMLDocument          = windows.GUID{Data1: 0xF7F3A506, Data2: 0x1E87, Data3: 0x42D6, Data4: [8]byte{0xBC, 0xFB, 0xB8, 0xC8, 0x09, 0xFA, 0x54, 0x94}}
	iidXMLDocumentIO        = windows.GUID{Data1: 0x6CD0E74E, Data2: 0xEE65, Data3: 0x4489, Data4: [8]byte{0x9E, 0xBF, 0xCA, 0x43, 0xE8, 0x7B, 0xA6, 0x37}}
)

// The methods called, by their place in each interface's table, after
// IInspectable's six.
const (
	createToastNotifierWithID = 7 // IToastNotificationManagerStatics
	getHistory                = 6 // IToastNotificationManagerStatics2
	createToastNotification   = 6 // IToastNotificationFactory
	showToast                 = 6 // IToastNotifier
	putTag                    = 6 // IToastNotification2
	putGroup                  = 8
	removeGroupedTagWithID    = 8 // IToastNotificationHistory
	loadXML                   = 6 // IXmlDocumentIO
)

// toastGroup holds every toast of Dens's, which their tags tell apart.
const toastGroup = "dens"

// comObject is a COM interface pointer: what it points to starts with its
// method table.
type comObject struct{ vtbl *[32]uintptr }

func (o *comObject) call(method int, args ...uintptr) error {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	if int32(r) < 0 {
		return fmt.Errorf("HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func (o *comObject) query(iid *windows.GUID) (*comObject, error) {
	var out *comObject
	err := o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, err
}

func (o *comObject) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl[2], uintptr(unsafe.Pointer(o)))
	}
}

// hstring is a Windows Runtime string.
type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	if r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h))); int32(r) < 0 {
		return 0, fmt.Errorf("WindowsCreateString: HRESULT 0x%08X", uint32(r))
	}
	return h, nil
}

func (h hstring) free() { procWindowsDeleteString.Call(uintptr(h)) }

// Notifier shows toasts under an app ID. Every call runs on one thread of
// Windows Runtime's multithreaded apartment.
type Notifier struct {
	appID  string
	work   chan func()
	done   chan struct{}
	clicks chan string
	once   sync.Once

	notifier, history, factory *comObject
}

// OpenNotifier starts toasts for appID, which a Start menu shortcut must
// carry for Windows to show them. A sandbox is Linux's.
func OpenNotifier(appID string, sandboxed bool) (*Notifier, error) {
	if sandboxed {
		return nil, errors.ErrUnsupported
	}
	if err := combase.Load(); err != nil {
		return nil, fmt.Errorf("no Windows Runtime: %v: %w", err, errors.ErrUnsupported)
	}
	n := &Notifier{appID: appID, work: make(chan func()), done: make(chan struct{}), clicks: make(chan string)}
	ready := make(chan error, 1)
	go n.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return n, nil
}

// run sets the toasts up on a thread of its own, then does the work it's
// given there until Close.
func (n *Notifier) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(n.done)
	// RO_INIT_MULTITHREADED. S_FALSE says the thread was already in it.
	if r, _, _ := procRoInitialize.Call(1); int32(r) < 0 {
		ready <- fmt.Errorf("RoInitialize: HRESULT 0x%08X", uint32(r))
		return
	}
	defer procRoUninitialize.Call()
	if err := n.setUp(); err != nil {
		n.notifier.release()
		n.history.release()
		n.factory.release()
		ready <- err
		return
	}
	ready <- nil
	for f := range n.work {
		f()
	}
	n.notifier.release()
	n.history.release()
	n.factory.release()
}

func (n *Notifier) setUp() error {
	manager, err := activationFactory("Windows.UI.Notifications.ToastNotificationManager", &iidToastManagerStatics)
	if err != nil {
		return err
	}
	defer manager.release()
	app, err := newHString(n.appID)
	if err != nil {
		return err
	}
	defer app.free()
	if err := manager.call(createToastNotifierWithID, uintptr(app), uintptr(unsafe.Pointer(&n.notifier))); err != nil {
		return fmt.Errorf("create the toasts' notifier: %w", err)
	}
	manager2, err := manager.query(&iidToastManagerStatics2)
	if err != nil {
		return fmt.Errorf("the toasts' history: %w", err)
	}
	defer manager2.release()
	if err := manager2.call(getHistory, uintptr(unsafe.Pointer(&n.history))); err != nil {
		return fmt.Errorf("the toasts' history: %w", err)
	}
	if n.factory, err = activationFactory("Windows.UI.Notifications.ToastNotification", &iidToastFactory); err != nil {
		return err
	}
	return nil
}

func activationFactory(class string, iid *windows.GUID) (*comObject, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()
	var f *comObject
	if r, _, _ := procRoGetActivationFactory.Call(uintptr(h), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f))); int32(r) < 0 {
		return nil, fmt.Errorf("%s: HRESULT 0x%08X", class, uint32(r))
	}
	return f, nil
}

// do runs f on the notifier's thread.
func (n *Notifier) do(f func() error) error {
	errc := make(chan error, 1)
	select {
	case n.work <- func() { errc <- f() }:
	case <-n.done:
		return errors.New("the notifier is closed")
	}
	return <-errc
}

// toastTag is a toast's tag for an ID of the helper's: a tag holds 64
// characters at most, and an ID can hold more.
func toastTag(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}

// toastXML is a toast's content: its heading and text, as text, and the
// address its click opens.
func toastXML(d DesktopNotification) string {
	esc := func(v string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(v))
		return b.String()
	}
	open := ""
	if d.Open != "" {
		open = ` activationType="protocol" launch="` + esc(d.Open) + `"`
	}
	return `<toast` + open + `><visual><binding template="ToastGeneric"><text>` + esc(d.Title) + `</text><text>` + esc(d.Body) +
		`</text></binding></visual><audio src="ms-winsoundevent:Notification.IM"/></toast>`
}

// Show shows a toast under id, replacing what shows under it.
func (n *Notifier) Show(id string, d DesktopNotification) error {
	return n.do(func() error {
		content, err := newHString(toastXML(d))
		if err != nil {
			return err
		}
		defer content.free()
		class, err := newHString("Windows.Data.Xml.Dom.XmlDocument")
		if err != nil {
			return err
		}
		defer class.free()
		var doc *comObject
		if r, _, _ := procRoActivateInstance.Call(uintptr(class), uintptr(unsafe.Pointer(&doc))); int32(r) < 0 {
			return fmt.Errorf("an XML document: HRESULT 0x%08X", uint32(r))
		}
		defer doc.release()
		docIO, err := doc.query(&iidXMLDocumentIO)
		if err != nil {
			return err
		}
		defer docIO.release()
		if err := docIO.call(loadXML, uintptr(content)); err != nil {
			return fmt.Errorf("the toast's content: %w", err)
		}
		xmlDoc, err := doc.query(&iidXMLDocument)
		if err != nil {
			return err
		}
		defer xmlDoc.release()
		var toast *comObject
		if err := n.factory.call(createToastNotification, uintptr(unsafe.Pointer(xmlDoc)), uintptr(unsafe.Pointer(&toast))); err != nil {
			return fmt.Errorf("a toast: %w", err)
		}
		defer toast.release()
		toast2, err := toast.query(&iidToastNotification2)
		if err != nil {
			return err
		}
		defer toast2.release()
		tag, err := newHString(toastTag(id))
		if err != nil {
			return err
		}
		defer tag.free()
		group, err := newHString(toastGroup)
		if err != nil {
			return err
		}
		defer group.free()
		if err := toast2.call(putTag, uintptr(tag)); err != nil {
			return err
		}
		if err := toast2.call(putGroup, uintptr(group)); err != nil {
			return err
		}
		if err := n.notifier.call(showToast, uintptr(unsafe.Pointer(toast))); err != nil {
			return fmt.Errorf("show the toast: %w", err)
		}
		return nil
	})
}

// Withdraw takes back the toast under id, from the Action Center too.
func (n *Notifier) Withdraw(id string) error {
	return n.do(func() error {
		tag, err := newHString(toastTag(id))
		if err != nil {
			return err
		}
		defer tag.free()
		group, err := newHString(toastGroup)
		if err != nil {
			return err
		}
		defer group.free()
		app, err := newHString(n.appID)
		if err != nil {
			return err
		}
		defer app.free()
		return n.history.call(removeGroupedTagWithID, uintptr(tag), uintptr(group), uintptr(app))
	})
}

// Clicks delivers nothing: a toast's click opens its address itself.
func (n *Notifier) Clicks() <-chan string { return n.clicks }

// Close stops the notifier; what shows stays.
func (n *Notifier) Close() error {
	n.once.Do(func() { close(n.work) })
	<-n.done
	return nil
}
