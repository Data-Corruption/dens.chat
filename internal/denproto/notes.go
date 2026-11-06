package denproto

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

// Notes to self (M17.3).
//
// Each member has notes in each den: a channel of kind notes naming them
// alone, which only they see, made as they first open it. Its messages
// are sealed as a DM's are, under a key made from the member's DM seal by
// HKDF, salted with the den's ID under a label of its own, as the identity
// is: every device holding the seal has it, with nothing to make, keep or
// hand over, and the den holds nothing of it. A message names the key by
// the channel's ID, in key_id, which its binding takes as a DM message's
// takes its key's. A member who starts over has another seal, so the notes
// sealed before no longer open.
//
// A Dens speaking a version of the protocol before 3 drops its connection
// on a channel of a kind it doesn't know, so a den sends notes only to one
// speaking 3.

// KindNotes is the channel kind of a member's notes.
const KindNotes = "notes"

const notesKeyContext = "dens-notes-v1"

// NotesKey makes the key a member's notes at a den are sealed with, from
// their seal.
func NotesKey(seal, denID []byte) (Bytes, error) {
	if len(seal) != SealSize || len(denID) != IDSize {
		return nil, errors.New("a notes key comes from a seal and a den's ID")
	}
	return hkdf.Key(sha256.New, seal, denID, notesKeyContext, DMKeySize)
}
