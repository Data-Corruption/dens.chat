package client

import (
	"encoding/json"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestPageMarksReachTheDen checks that a join's and a mute's marks, as the
// page sends them, reach what the service sends the den whole.
func TestPageMarksReachTheDen(t *testing.T) {
	for _, c := range []struct {
		frame string
		want  denproto.VoiceMute
	}{
		{`{"t":"voice.join","d":{"den":"d","channel":"41","muted":true,"deafened":false,"away":true,"sealed":true}}`,
			denproto.VoiceMute{Muted: true, Away: true}},
		{`{"t":"voice.mute","d":{"den":"d","muted":true,"deafened":true,"away":true}}`,
			denproto.VoiceMute{Muted: true, Deafened: true, Away: true}},
		{`{"t":"voice.mute","d":{"den":"d","muted":false,"deafened":false,"away":false}}`, denproto.VoiceMute{}},
	} {
		var msg pageMessage
		if err := json.Unmarshal([]byte(c.frame), &msg); err != nil {
			t.Fatal(err)
		}
		if got := msg.marks(); got != c.want {
			t.Errorf("%s: marks %+v, want %+v", c.frame, got, c.want)
		}
	}
}
