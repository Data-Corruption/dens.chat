package denclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
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
