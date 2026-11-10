package denclient

import (
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A den's member is taken with a name style in a font this Dens doesn't
// have, which a later den may send and the page shows as none, and refused
// with anything that isn't a font's name or a color.
func TestCleanMemberChecksTheNameStyle(t *testing.T) {
	member := func(s denproto.NameStyle) denproto.Member {
		return denproto.Member{ID: "7", Username: "bob", DisplayName: "Bob", Role: denproto.RoleMember, JoinedAt: 1, NameStyle: s}
	}
	for _, s := range []denproto.NameStyle{{}, {Font: "pacifico"}, {Font: "a-font-to-come", Color: "#aa0000"}, {Color: "#aa0000", Color2: "#00aa00"}} {
		if m, ok := cleanMember(member(s)); !ok || m.NameStyle != s {
			t.Errorf("a member styled %+v: %+v, %t", s, m.NameStyle, ok)
		}
	}
	for _, s := range []denproto.NameStyle{{Font: "Comic Sans"}, {Font: "x;y"}, {Color: "red"}, {Color: "#AA0000"},
		{Color: "#aa0000", Color2: "url(https://evil.example/x.png)"}, {Color2: "#00aa00"}} {
		if _, ok := cleanMember(member(s)); ok {
			t.Errorf("a member styled %+v was taken", s)
		}
	}
}
