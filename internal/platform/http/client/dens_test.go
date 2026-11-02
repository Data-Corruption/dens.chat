package client

import (
	"net/http"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A den's refusal reaches the page in the page's own words, never the
// den's, since dens are other people's servers; a sound's taken name says
// so, rather than that a detail was wrong (M14.3).
func TestADensRefusalIsSaidInThePagesWords(t *testing.T) {
	for code, want := range map[string]string{
		denproto.CodeSoundNameTaken: "Another sound in this den has that name.",
		denproto.CodeInvalidField:   "The den rejected one of the details you entered.",
	} {
		e := &denproto.Error{Status: http.StatusConflict, Code: code, Message: "<b>the den's own words</b>"}
		if got := denMessage(e); got != want {
			t.Errorf("%s: %q, want %q", code, got, want)
		}
	}
}
