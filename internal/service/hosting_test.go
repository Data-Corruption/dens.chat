package service_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/control"
)

const localPassword = "correct horse battery"

// freeUDPPort finds a UDP port free on every interface, as the media port
// binds, among the tests' ports.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	return findPort(t, func(port int) error {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
		if err == nil {
			conn.Close()
		}
		return err
	})
}

// browser is a paired browser on a development instance's page.
type browser struct {
	t    *testing.T
	base string
	http *http.Client
}

// pair pairs a browser and sets the local password.
func pair(t *testing.T, a *app.App, client control.Client) *browser {
	t.Helper()
	b := pairOnly(t, a, client)
	b.expect(http.StatusOK, "set the password", "POST", "/api/password", map[string]string{"password": localPassword})
	return b
}

// pairOnly pairs a browser with an instance whose local password is set,
// as one restored from a backup is.
func pairOnly(t *testing.T, a *app.App, client control.Client) *browser {
	t.Helper()
	var p control.Pair
	if err := client.Call(control.Request{Op: control.OpPair}, &p); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	b := &browser{t: t, base: fmt.Sprintf("http://127.0.0.1:%d", a.Instance.ClientPort), http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	b.expect(http.StatusOK, "pair", "POST", "/api/pair", map[string]string{"token": p.URL[strings.Index(p.URL, "#token=")+len("#token="):]})
	return b
}

func (b *browser) do(method, path string, body any) (int, []byte) {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, b.base+path, reader)
	req.Header.Set("Origin", b.base)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.http.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// expect makes a request, fails unless it answers code, and decodes the
// answer into the last of out, if given.
func (b *browser) expect(code int, what, method, path string, body any, out ...any) string {
	b.t.Helper()
	got, data := b.do(method, path, body)
	if got != code {
		b.t.Fatalf("%s: %d %s, want %d", what, got, data, code)
	}
	if len(out) > 0 {
		if err := json.Unmarshal(data, out[0]); err != nil {
			b.t.Fatalf("%s: decode %s: %v", what, data, err)
		}
	}
	return string(data)
}

type hostingView struct {
	Enabled  bool   `json:"enabled"`
	Serving  bool   `json:"serving"`
	Problem  string `json:"problem"`
	DenPort  int    `json:"den_port"`
	Den      string `json:"den"`
	URL      string `json:"url"`
	Exported int64  `json:"exported"`
}

// hosting is hosting as the settings see it.
func (b *browser) hosting() hostingView {
	b.t.Helper()
	var h hostingView
	b.expect(http.StatusOK, "hosting", "GET", "/api/hosting", nil, &h)
	return h
}

// setHosting turns hosting on or off, and returns hosting as it is then.
func (b *browser) setHosting(on bool, password string) hostingView {
	b.t.Helper()
	var h hostingView
	b.expect(http.StatusOK, fmt.Sprintf("turn hosting on: %v", on), "POST", "/api/hosting", map[string]any{"on": on, "password": password}, &h)
	return h
}

type densView struct {
	Dens []struct {
		Own   bool   `json:"own"`
		State string `json:"state"`
	} `json:"dens"`
	Hosting struct {
		Enabled  bool  `json:"enabled"`
		Created  bool  `json:"created"`
		Owned    bool  `json:"owned"`
		Exported int64 `json:"exported"`
	} `json:"hosting"`
}

// dens is the dens view, as home shows it.
func (b *browser) dens() densView {
	b.t.Helper()
	var v densView
	b.expect(http.StatusOK, "the dens view", "GET", "/api/dens", nil, &v)
	return v
}

// ownState is the state of the den this instance hosts, as its page sees
// it, or "" before it has joined it.
func (b *browser) ownState() string {
	b.t.Helper()
	for _, d := range b.dens().Dens {
		if d.Own {
			return d.State
		}
	}
	return ""
}

func (b *browser) waitOwnState(want string) {
	b.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		got := b.ownState()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			b.t.Fatalf("own den is %q, want %q", got, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// answers says whether the den listener on port takes a request.
func answers(port int) bool {
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// mediaPortsFree says whether nothing holds the media ports, by binding
// them as the den does.
func mediaPortsFree(t *testing.T, udp, tcp int) bool {
	t.Helper()
	u, err := net.ListenUDP("udp", &net.UDPAddr{Port: udp})
	if err != nil {
		return false
	}
	u.Close()
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", tcp))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// A new instance hosts nothing until its owner turns hosting on (M9.1).
func TestHostingStartsOff(t *testing.T) {
	a, client := startDevService(t)
	b := pair(t, a, client)
	if h := b.hosting(); h.Enabled || h.Serving || h.DenPort != a.Instance.Den.Port {
		t.Fatalf("a new instance's hosting is %+v", h)
	}
	if strings.Contains(b.expect(http.StatusOK, "the dens view", "GET", "/api/dens", nil), `"enabled":true`) {
		t.Fatal("the dens view says hosting is on")
	}
	b.expect(http.StatusConflict, "create a den with hosting off", "POST", "/api/den",
		map[string]string{"name": "Den", "url": "https://den.test", "username": "alice", "display_name": "Alice", "password": "den password"})
	var status control.Status
	if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil || status.DenEnabled {
		t.Fatalf("status %+v, %v", status, err)
	}
}

// Hosting turns on and off from the page: on binds the den's ports and
// serves the den, off closes them and its sockets and keeps the den, and on
// again serves it as it was. Turning it on takes the local password, and
// fails, naming the port and the flag that moves it, while another program
// holds a port.
func TestHostingTurnsOnAndOff(t *testing.T) {
	den, udp, tcp := freePort(t), freeUDPPort(t), freePort(t)
	a, client := startDevServiceWith(t, func(o *app.OpenOptions) {
		o.DevDenPort, o.DevMediaPorts = den, [2]int{udp, tcp}
	})
	b := pair(t, a, client)
	// A development instance given a den port hosts at once.
	if h := b.hosting(); !h.Enabled || !h.Serving || !answers(den) {
		t.Fatalf("hosting is %+v, and the den answers: %v", h, answers(den))
	}
	b.expect(http.StatusOK, "create the den", "POST", "/api/den",
		map[string]string{"name": "Den", "url": "https://den.test", "username": "alice", "display_name": "Alice", "password": "den password"})
	b.waitOwnState("connected")

	if h := b.setHosting(false, ""); h.Enabled || h.Serving {
		t.Fatalf("after turning off, hosting is %+v", h)
	}
	if answers(den) {
		t.Fatal("the den listener answers with hosting off")
	}
	if !mediaPortsFree(t, udp, tcp) {
		t.Fatal("the media ports are held with hosting off")
	}
	b.waitOwnState("offline")
	var status control.Status
	if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil || status.DenEnabled {
		t.Fatalf("status %+v, %v", status, err)
	}
	err := client.Call(control.Request{Op: control.OpOwnerInvite, Password: localPassword}, &control.OwnerInvite{})
	if err == nil || !strings.Contains(err.Error(), "hosting is off") {
		t.Fatalf("an owner invite with hosting off: %v", err)
	}

	b.expect(http.StatusForbidden, "turn hosting on with the wrong password", "POST", "/api/hosting",
		map[string]any{"on": true, "password": "wrong wrong wrong"})
	held, err := net.ListenUDP("udp", &net.UDPAddr{Port: udp})
	if err != nil {
		t.Fatal(err)
	}
	problem := b.expect(http.StatusConflict, "turn hosting on with a port held", "POST", "/api/hosting",
		map[string]any{"on": true, "password": localPassword})
	held.Close()
	for _, want := range []string{fmt.Sprint(udp), "UDP", "--media-udp-port", "-MediaUdpPort"} {
		if !strings.Contains(problem, want) {
			t.Errorf("%s doesn't say %s", problem, want)
		}
	}
	if h := b.hosting(); h.Enabled || h.Serving || answers(den) {
		t.Fatalf("a failed turn on left hosting %+v", h)
	}

	if h := b.setHosting(true, localPassword); !h.Enabled || !h.Serving || !answers(den) {
		t.Fatalf("after turning on, hosting is %+v", h)
	}
	// The den is as it was, and the page's member back in it.
	b.waitOwnState("connected")
	if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil || !status.DenEnabled {
		t.Fatalf("status %+v, %v", status, err)
	}
}

// Hosting that's on but can't start as the service does leaves the rest
// serving, and the page says why; once the port is free, turning hosting
// on starts the den.
func TestHostingThatCantStartSaysWhy(t *testing.T) {
	held, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	den := held.Addr().(*net.TCPAddr).Port
	a, client := startDevServiceWith(t, func(o *app.OpenOptions) {
		o.DevDenPort, o.DevMediaPorts = den, [2]int{freeUDPPort(t), freePort(t)}
	})
	b := pair(t, a, client)
	if h := b.hosting(); !h.Enabled || h.Serving || !strings.Contains(h.Problem, fmt.Sprint(den)) || !strings.Contains(h.Problem, "--den-port") {
		t.Fatalf("hosting with its port held is %+v", h)
	}
	var status control.Status
	if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil || !status.DenEnabled || !strings.Contains(status.DenProblem, fmt.Sprint(den)) {
		t.Fatalf("status with the den's port held: %+v, %v", status, err)
	}
	held.Close()
	if h := b.setHosting(true, localPassword); !h.Serving || h.Problem != "" || !answers(den) {
		t.Fatalf("hosting is %+v once its port is free", h)
	}
}

// dens den hosting turns hosting on and off over the control endpoint, as
// the page's switch does (M10): on takes the local password, says where
// Caddy and the router send the den, and fails, naming the port and the
// flag that moves it, while another program holds one; off needs nothing
// more, since the command asks first.
func TestHostingFromTheCommandLine(t *testing.T) {
	den, udp, tcp := freePort(t), freeUDPPort(t), freePort(t)
	a, client := startDevServiceWith(t, func(o *app.OpenOptions) {
		o.DevDenPort, o.DevMediaPorts = den, [2]int{udp, tcp}
	})
	b := pair(t, a, client)
	var h control.Hosting
	if err := client.Call(control.Request{Op: control.OpHosting, On: false}, &h); err != nil || h.On || h.Serving || answers(den) {
		t.Fatalf("turning hosting off: %+v, %v", h, err)
	}
	if err := client.Call(control.Request{Op: control.OpHosting, On: true, Password: "wrong wrong wrong"}, &h); err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("turning hosting on with the wrong password: %v", err)
	}
	held, err := net.Listen("tcp", fmt.Sprintf(":%d", tcp))
	if err != nil {
		t.Fatal(err)
	}
	err = client.Call(control.Request{Op: control.OpHosting, On: true, Password: localPassword}, &h)
	held.Close()
	for _, want := range []string{fmt.Sprint(tcp), "TCP", "--media-tcp-port"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("turning hosting on with a port held: %v, without %s", err, want)
		}
	}
	if b.hosting().Enabled {
		t.Fatal("a failed turn on left hosting on")
	}
	// The password's limiter lets three checks through at once.
	time.Sleep(2500 * time.Millisecond)
	if err := client.Call(control.Request{Op: control.OpHosting, On: true, Password: localPassword}, &h); err != nil {
		t.Fatal(err)
	}
	if !h.On || !h.Serving || h.DenPort != den || h.MediaUDPPort != udp || h.MediaTCPPort != tcp || h.Den != "" || !answers(den) {
		t.Fatalf("hosting turned on from the command line: %+v", h)
	}
}
