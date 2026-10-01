package denclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// The DM seal (M1.7): 32 random bytes that seal this member's copies of
// their DM keys on a den. It never leaves their devices, except sealed for
// a new one of theirs. An install keeps one it starts new dens with; each
// den keeps its own, which is the same one unless this device got it from
// another of the member's, or the member started over there.

// setSeal gives this device a den's seal, keeps it, and moves the den's
// waiting exchanges on with it.
func (c *conn) setSeal(ctx context.Context, seal []byte) error {
	secret, err := vault.NewSecret(seal)
	if err != nil {
		return err
	}
	if err := storeSeal(ctx, c.m.db, c.m.v, c.j.denID, seal); err != nil {
		secret.Close()
		return err
	}
	if have, err := installSeal(ctx, c.m.db, c.m.v); err == nil && have == nil {
		if err := setInstallSeal(ctx, c.m.db, c.m.v, seal); err != nil {
			c.m.log.Errorf("keep the DM seal for new dens: %v", err)
		}
	} else {
		clear(have)
	}
	c.mu.Lock()
	old := c.j.seal
	c.j.seal = secret
	if c.den != nil {
		c.markDueLocked(c.dueKeysLocked()...)
	}
	c.mu.Unlock()
	if old != nil {
		old.Close()
	}
	c.m.notify()
	// Messages the page couldn't open may open now.
	c.m.publish(PageEvent{DenID: c.j.denID.String(), Reset: true})
	return nil
}

// TypeSeal gives this device the member's DM seal at a den, as they saved
// it: after a recovery code, with no other device to hand it over.
func (m *Manager) TypeSeal(ctx context.Context, denID, typed string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	seal, err := denproto.ParseSeal(typed)
	if err != nil {
		return inputError(err)
	}
	defer clear(seal)
	check, err := denproto.SealCheck(seal, c.j.denID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	var want []byte
	if c.den != nil {
		want = c.den.sealCheck
	}
	c.mu.Unlock()
	if want == nil {
		return inputError(errors.New("this den isn't connected yet; try again once it is"))
	}
	if !denproto.Equal(check, want) {
		return inputError(errors.New("that isn't your DM seal for this den. Check it against what you saved; if you can't find it, start over"))
	}
	return c.setSeal(ctx, seal)
}

// StartOver gives the member a new DM seal at a den, for when they've lost
// theirs and every device that held it, or to shut out a stolen device.
// It takes their den password. The keys of their DMs there retire, what
// they sealed stays unreadable to this member, and each DM needs a new
// check before anything more is sent in it; their other devices there are
// signed out. It returns the new seal, which is shown once.
func (m *Manager) StartOver(ctx context.Context, denID, password string) (string, error) {
	c, err := m.find(denID)
	if err != nil {
		return "", err
	}
	seal := denproto.NewSeal()
	defer clear(seal)
	check, err := denproto.SealCheck(seal, c.j.denID)
	if err != nil {
		return "", err
	}
	req := denproto.StartOverRequest{Verifier: denproto.Verifier(password, c.j.denID, c.status().Username), SealCheck: check}
	var out denproto.StartedOver
	if err := c.call(ctx, http.MethodPost, "/api/me/seal", req, &out); err != nil {
		return "", err
	}
	c.mu.Lock()
	if c.den != nil {
		c.den.sealCheck = check
	}
	c.mu.Unlock()
	if err := c.setSeal(ctx, seal); err != nil {
		return "", err
	}
	// Dens this install joins from now on start with it too.
	if err := setInstallSeal(ctx, m.db, m.v, seal); err != nil {
		m.log.Errorf("keep the DM seal for new dens: %v", err)
	}
	m.log.Infof("Started over with a new DM seal at a den")
	return denproto.FormatSeal(seal), nil
}

// ShowSeal returns the member's DM seal at a den, to save. The caller has
// checked the local password first.
func (m *Manager) ShowSeal(denID string) (string, error) {
	c, err := m.find(denID)
	if err != nil {
		return "", err
	}
	var shown string
	if err := c.useSeal(func(seal []byte) error {
		shown = denproto.FormatSeal(seal)
		return nil
	}); err != nil {
		return "", err
	}
	return shown, nil
}
