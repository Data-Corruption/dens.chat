package denproto

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// A file's digests and its preview's are as many as their sizes give, the
// preview's with its size; a file without opens as one from v1.15.0 did.
func TestFileDigestsFitTheirSizes(t *testing.T) {
	key := Random(DMKeySize)
	ad := MessageAD{DenID: testDen, Channel: 5, Author: 2, Nonce: Random(16), KeyID: 9, Revision: 1}
	sums := func(size int64) Bytes { return Random(vault.SumsSize(vault.SealedSize(size))) }
	file := func(size int64, fileSums Bytes, thumb *DMThumb) DMPayload {
		return DMPayload{Files: []DMFile{{ID: "10", Key: Random(32), Name: "a.png", Type: "image/png", Size: size, Width: 1, Height: 1,
			Thumb: thumb, Sums: fileSums}}}
	}
	thumb := func(size int64, s Bytes) *DMThumb {
		return &DMThumb{ID: "11", Width: 1, Height: 1, Size: size, Sums: s}
	}
	for name, c := range map[string]struct {
		p  DMPayload
		ok bool
	}{
		"none":                         {file(40<<20, nil, thumb(0, nil)), true},
		"both":                         {file(40<<20, sums(40<<20), thumb(3000, sums(3000))), true},
		"an empty file's":              {file(0, sums(0), nil), true},
		"the largest file's":           {file(MaxFileSize, sums(MaxFileSize), nil), true},
		"a file's, too few":            {file(40<<20, sums(1<<20), nil), false},
		"a file's, too many":           {file(1<<20, sums(40<<20), nil), false},
		"a preview's, too many":        {file(1<<20, nil, thumb(3000, sums(40<<20))), false},
		"a preview's, without a size":  {file(1<<20, nil, thumb(0, sums(3000))), false},
		"a preview's, past any file's": {file(1<<20, nil, thumb(MaxFileSize+1, sums(MaxFileSize+1))), false},
	} {
		sealed, err := SealDMMessage(key, ad, c.p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := OpenDMMessage(key, ad, sealed); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Ten files of the largest size, each with its 32 digests and its
// preview's, seal within a group's message beside its longest text, a
// grant naming every other member and a forward's mark, every name and
// text at what JSON makes longest of it.
func TestTenLargestFilesFit(t *testing.T) {
	alice, err := DeriveIdentity(NewSeal(), testDen)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	id := func(n int) string { return strconv.FormatInt(1<<62+int64(n), 10) }
	nonce := Random(16)
	ad := MessageAD{DenID: testDen, Channel: 1 << 62, Author: 1 << 62, Nonce: nonce, KeyID: 1 << 62, Revision: 1 << 30}
	var editors []string
	for i := range MaxGroupMembers - 1 {
		editors = append(editors, id(100+i))
	}
	grant, err := alice.SignGrant(testDen, 1<<62, 1<<62, nonce, 1<<30, editors)
	if err != nil {
		t.Fatal(err)
	}
	p := DMPayload{Text: strings.Repeat("\x01", MaxTextRunes), Grant: &grant, Forwarded: true}
	for i := range MaxAttachments {
		thumbSize := int64(16 * vault.StreamChunk)
		p.Files = append(p.Files, DMFile{ID: id(i), Key: Random(32), Name: strings.Repeat(`"`, MaxFilenameBytes),
			Type: "x/" + strings.Repeat("y", maxMediaType-2), Size: MaxFileSize, Width: MaxImageSide, Height: MaxImageSide,
			Animated: true, Duration: MaxDuration, Spoiler: true, Sums: Random(vault.SumsSize(vault.SealedSize(MaxFileSize))),
			Thumb: &DMThumb{ID: id(50 + i), Width: MaxThumb, Height: MaxThumb, Size: thumbSize,
				Sums: Random(vault.SumsSize(vault.SealedSize(thumbSize)))}})
	}
	if err := CheckDMPayload(p); err != nil {
		t.Fatal(err)
	}
	sealed, err := SealGroupMessage(Random(DMKeySize), ad, alice, 1<<62, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d bytes sealed of %d", len(sealed), MaxSealed)
	if bytes.Contains(sealed, []byte(p.Files[0].Name)) {
		t.Fatal("it isn't sealed")
	}
}
