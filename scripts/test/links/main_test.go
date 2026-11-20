package main

import "testing"

func TestSlug(t *testing.T) {
	for _, tc := range []struct{ heading, want string }{
		{"Files (M1.4, M1.8)", "files-m14-m18"},
		{"The member's record (M16.1)", "the-members-record-m161"},
		{"Sending, editing, deleting", "sending-editing-deleting"},
		{"Dens — Design Doc", "dens--design-doc"},
		{"`Dens-Protocol` and *the range*", "dens-protocol-and-the-range"},
		{"[Relay](protocol/auth.md#relay)'s boxes", "relays-boxes"},
		{"name_style, café", "name_style-café"},
	} {
		if got := Slug(tc.heading); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.heading, got, tc.want)
		}
	}
}

func TestParse(t *testing.T) {
	d := parse("# Added\n\n```\n# Not a heading [x](nowhere.md)\n```\n\n## Added\n\nSee `[y](gone.md)` and [z](#added-1).\n")
	for _, a := range []string{"added", "added-1"} {
		if !d.anchors[a] {
			t.Errorf("no anchor %q in %v", a, d.anchors)
		}
	}
	if len(d.anchors) != 2 {
		t.Errorf("anchors = %v, want two", d.anchors)
	}
	if m := inlineLink.FindAllStringSubmatch(d.text, -1); len(m) != 1 || m[0][2] != "#added-1" {
		t.Errorf("links = %v, want only #added-1", m)
	}
}
