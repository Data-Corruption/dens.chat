package denclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/coder/websocket"
)

// fakeDen answers as a den speaking the versions lo to hi, recording the
// version each request came at.
func fakeDen(t *testing.T, lo, hi int) (*httptest.Server, func() []int) {
	t.Helper()
	var mu sync.Mutex
	var seen []int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := strconv.Atoi(r.Header.Get(denproto.HeaderVersion))
		mu.Lock()
		seen = append(seen, v)
		mu.Unlock()
		w.Header().Set(denproto.HeaderRange, strconv.Itoa(lo)+"-"+strconv.Itoa(hi))
		if v < lo || v > hi {
			denproto.WriteError(w, denproto.Errorf(http.StatusUpgradeRequired, denproto.CodeProtocolUnsupported, "no"))
			return
		}
		if r.URL.Path == "/api/ws" {
			if ws, err := websocket.Accept(w, r, nil); err == nil {
				ws.Close(websocket.StatusNormalClosure, "")
			}
			return
		}
		denproto.WriteJSON(w, http.StatusOK, map[string]int{"version": v})
	}))
	t.Cleanup(s.Close)
	return s, func() []int {
		mu.Lock()
		defer mu.Unlock()
		return append([]int(nil), seen...)
	}
}

// A den that speaks only versions older than this build's is asked again,
// once, at the newest it speaks, and requests go at that from then on,
// until it says it speaks a newer one, as once it updates (M16.3).
func TestVersionsNegotiated(t *testing.T) {
	ctx := context.Background()
	old, seen := fakeDen(t, 1, 1)
	a := &api{base: old.URL, client: old.Client()}
	var got map[string]int
	if err := a.call(ctx, http.MethodGet, "/x", nil, nil, &got); err != nil || got["version"] != 1 {
		t.Fatalf("a call to a den speaking 1: %v, %v", got, err)
	}
	if err := a.call(ctx, http.MethodPost, "/x", nil, map[string]string{"a": "b"}, &got); err != nil || got["version"] != 1 {
		t.Fatalf("a second call: %v, %v", got, err)
	}
	if v := seen(); len(v) != 3 || v[0] != denproto.Version || v[1] != 1 || v[2] != 1 {
		t.Errorf("the den was asked at %v", v)
	}

	// The same den, updated.
	updated, _ := fakeDen(t, 1, denproto.Version)
	a.base = updated.URL
	if err := a.call(ctx, http.MethodGet, "/x", nil, nil, &got); err != nil || got["version"] != 1 || a.speaks() != denproto.Version {
		t.Fatalf("after the den updated: %v, version %d, %v", got, a.speaks(), err)
	}

	// A den with nothing in common is refused, once.
	newer, seenNewer := fakeDen(t, denproto.Version+1, denproto.Version+2)
	b := &api{base: newer.URL, client: newer.Client()}
	if err := b.call(ctx, http.MethodGet, "/x", nil, nil, &got); !denproto.IsCode(err, denproto.CodeProtocolUnsupported) {
		t.Errorf("a den speaking only newer versions: %v", err)
	}
	if v := seenNewer(); len(v) != 1 {
		t.Errorf("a den speaking only newer versions was asked %d times", len(v))
	}
}

// A socket opens at the version the den and this build share: one dialed
// at an older version than a den that updated since speaks dials again at
// the newer, rather than going without group DMs until it reconnects, and
// one a den older than this build refuses dials again at the den's (M16.3).
func TestSocketDialsAtTheSharedVersion(t *testing.T) {
	ctx := context.Background()
	updated, seen := fakeDen(t, 1, denproto.Version)
	a := &api{base: updated.URL, client: updated.Client()}
	a.version.Store(1) // as last heard, from the den before it updated
	ws, _, speaking, err := dialSocket(ctx, a, updated.URL+"/api/ws", nil, websocket.DialOptions{HTTPClient: updated.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ws.CloseNow()
	if v := seen(); speaking != denproto.Version || len(v) != 2 || v[0] != 1 || v[1] != denproto.Version {
		t.Errorf("the socket went at %d, after upgrades at %v", speaking, v)
	}

	old, seenOld := fakeDen(t, 1, 1)
	b := &api{base: old.URL, client: old.Client()}
	ws, _, speaking, err = dialSocket(ctx, b, old.URL+"/api/ws", nil, websocket.DialOptions{HTTPClient: old.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ws.CloseNow()
	if v := seenOld(); speaking != 1 || len(v) != 2 || v[0] != denproto.Version || v[1] != 1 {
		t.Errorf("the socket to an older den went at %d, after upgrades at %v", speaking, v)
	}
}
