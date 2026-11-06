package den

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// The den speaks versions 1 to 3 of the protocol, and makes a member's
// notes only at 3 (M17.3): at 2 they're no route of its, as group DMs
// are none at 1.
func TestNotesNeedVersion3(t *testing.T) {
	f := newFixture(t)
	token, _ := f.owner()
	open := func(version int) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/api/notes", nil)
		req.Header.Set(denproto.HeaderVersion, strconv.Itoa(version))
		req.Header.Set("Authorization", "Bearer "+token.String())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	wantError(t, open(denproto.GroupsVersion), http.StatusNotFound, denproto.CodeNotFound)
	wantError(t, open(denproto.NotesVersion+1), http.StatusUpgradeRequired, denproto.CodeProtocolUnsupported)
	resp := open(denproto.NotesVersion)
	defer resp.Body.Close()
	var notes denproto.Channel
	if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil || resp.StatusCode != http.StatusOK || notes.Kind != denproto.KindNotes ||
		len(notes.Members) != 1 {
		t.Fatalf("the owner's notes at version 3: %d %+v, %v", resp.StatusCode, notes, err)
	}
	if got := resp.Header.Get(denproto.HeaderRange); got != "1-3" {
		t.Errorf("the den's range: %q", got)
	}
}
