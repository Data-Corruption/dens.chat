package den

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A v1.15.0 Dens still offers group DMs, and asks the den for them as it
// did; the den answers every such request that they're gone, which that
// Dens shows as something no longer on the den.
func TestGroupDMsAreGone(t *testing.T) {
	f := newFixture(t)
	token, _ := f.owner()
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/group-dms", ""},
		{http.MethodGet, "/api/group-dms/5/changes?after=0", ""},
		{http.MethodPost, "/api/group-dms/5/changes", `{"change":{}}`},
	} {
		for _, version := range []int{denproto.GroupsVersion, denproto.NotesVersion} {
			req, _ := http.NewRequest(r.method, f.srv.URL+r.path, strings.NewReader(r.body))
			req.Header.Set(denproto.HeaderVersion, strconv.Itoa(version))
			req.Header.Set("Authorization", "Bearer "+token.String())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			err = denproto.ReadError(resp)
			resp.Body.Close()
			var perr *denproto.Error
			if resp.StatusCode != http.StatusGone || !denproto.IsCode(err, denproto.CodeNotFound) || !errors.As(err, &perr) ||
				!strings.Contains(perr.Message, "group DMs") {
				t.Errorf("%s %s at version %d: %d %v", r.method, r.path, version, resp.StatusCode, err)
			}
		}
	}
}
