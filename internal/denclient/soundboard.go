package denclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// The den's soundboard (M14.3): staff's page adds a sound by an upload its
// page prepared, which went up as any upload does, and names it; staff
// rename and delete sounds; and members play them in calls (voice.go).

// AddSound adds a sound to a den's soundboard: an upload of this member's,
// waiting to be used, and its name.
func (m *Manager) AddSound(ctx context.Context, denID, name, uploadID string) (denproto.Sound, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Sound{}, err
	}
	if err := checkID("upload", uploadID); err != nil {
		return denproto.Sound{}, err
	}
	clean, err := denproto.CleanSoundName(name)
	if err != nil {
		return denproto.Sound{}, inputError(errors.New("A sound's name is 1 to 32 characters."))
	}
	var sound denproto.Sound
	if err := c.call(ctx, http.MethodPost, "/api/soundboard", denproto.SoundRequest{Name: clean, FileID: uploadID}, &sound); err != nil {
		return denproto.Sound{}, err
	}
	if denproto.CheckSound(sound) != nil {
		return denproto.Sound{}, errMalformed
	}
	return sound, nil
}

// RenameSound gives one of a den's sounds another name.
func (m *Manager) RenameSound(ctx context.Context, denID, soundID, name string) (denproto.Sound, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Sound{}, err
	}
	if err := checkID("sound", soundID); err != nil {
		return denproto.Sound{}, err
	}
	clean, err := denproto.CleanSoundName(name)
	if err != nil {
		return denproto.Sound{}, inputError(errors.New("A sound's name is 1 to 32 characters."))
	}
	var sound denproto.Sound
	if err := c.call(ctx, http.MethodPatch, "/api/soundboard/"+soundID, denproto.SoundUpdate{Name: clean}, &sound); err != nil {
		return denproto.Sound{}, err
	}
	if denproto.CheckSound(sound) != nil || sound.ID != soundID {
		return denproto.Sound{}, errMalformed
	}
	return sound, nil
}

// DeleteSound deletes one of a den's sounds, and its file.
func (m *Manager) DeleteSound(ctx context.Context, denID, soundID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("sound", soundID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/soundboard/"+soundID, nil, nil)
}
