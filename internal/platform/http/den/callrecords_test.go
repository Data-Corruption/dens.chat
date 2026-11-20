package den

import (
	"net/http"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Staff read and reset a voice channel's record over HTTP, and a
// member gets 403 for either.
func TestCallRecordOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.owner()
	member := f.member(owner, "bob")
	name := "Lounge"
	var lounge denproto.Channel
	if code := f.call(http.MethodPost, "/api/channels", owner, denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice}, &lounge); code != http.StatusCreated {
		t.Fatalf("creating the voice channel: %d", code)
	}
	path := "/api/channels/" + lounge.ID + "/record"
	var rec denproto.CallRecord
	if code := f.call(http.MethodGet, path, owner, nil, &rec); code != http.StatusOK || rec.RecordMS != 0 {
		t.Fatalf("staff read %d %+v", code, rec)
	}
	if code := f.call(http.MethodDelete, path, owner, nil, nil); code != http.StatusNoContent {
		t.Fatalf("staff reset: %d", code)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if code := f.call(method, path, member, nil, nil); code != http.StatusForbidden {
			t.Fatalf("a member's %s: %d", method, code)
		}
	}
}
