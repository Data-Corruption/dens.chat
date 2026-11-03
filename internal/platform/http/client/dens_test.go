package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A den's refusal reaches the page in the page's own words, never the
// den's, since dens are other people's servers; a sound's or an emoji's
// taken name says so, rather than that a detail was wrong (M14.3, M15.2).
func TestADensRefusalIsSaidInThePagesWords(t *testing.T) {
	for code, want := range map[string]string{
		denproto.CodeSoundNameTaken: "Another sound in this den has that name.",
		denproto.CodeEmojiNameTaken: "Another emoji in this den has that name.",
		denproto.CodeInvalidField:   "The den rejected one of the details you entered.",
	} {
		e := &denproto.Error{Status: http.StatusConflict, Code: code, Message: "<b>the den's own words</b>"}
		if got := denMessage(e); got != want {
			t.Errorf("%s: %q, want %q", code, got, want)
		}
	}
}

// A failure on this computer that wraps an errno, as the media worker not
// starting does, isn't a den out of reach, though syscall.Errno has
// net.Error's methods; a dial the den refused still is, and so is a
// deadline waiting on the den.
func TestALocalFailureIsntADenOutOfReach(t *testing.T) {
	rt := &router{}
	answer := func(err error) (int, string) {
		w := httptest.NewRecorder()
		rt.denError(w, httptest.NewRequest(http.MethodPost, "/api/dens/x/uploads", nil), err)
		return w.Code, w.Body.String()
	}
	local := fmt.Errorf("start the media worker: set idle scheduling: %w", syscall.EPERM)
	if code, body := answer(local); code != http.StatusInternalServerError || strings.Contains(body, "reach the den") {
		t.Errorf("a local failure answered %d %q", code, body)
	}
	refused := &url.Error{Op: "Post", URL: "http://127.0.0.1:1/api/uploads", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}}
	if code, body := answer(refused); code != http.StatusBadGateway {
		t.Errorf("a refused dial answered %d %q", code, body)
	}
	waited := fmt.Errorf("wait for the den: %w", context.DeadlineExceeded)
	if code, body := answer(waited); code != http.StatusBadGateway || !strings.Contains(body, "in time") {
		t.Errorf("a deadline waiting on the den answered %d %q", code, body)
	}
}
