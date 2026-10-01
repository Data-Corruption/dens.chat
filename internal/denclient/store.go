package denclient

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Profile is what this install knows about a joined den and its account
// there. It is sealed with the data key: which dens someone belongs to is
// nobody else's business.
type Profile struct {
	URL    string          `json:"url"`
	Name   string          `json:"name"`
	Member denproto.Member `json:"member"`
}

type joined struct {
	denID    denproto.Bytes
	profile  Profile
	key      *vault.SigningKey
	joinedAt time.Time
	// seal is the member's DM seal at this den, or nil while this device
	// has none.
	seal *vault.Secret
}

// close releases the keys in locked memory.
func (j *joined) close() {
	j.key.Close()
	if j.seal != nil {
		j.seal.Close()
	}
}

func profileAD(denID []byte) []byte { return append([]byte("joined_dens.profile:"), denID...) }
func keyAD(denID []byte) []byte     { return append([]byte("joined_dens.device_key:"), denID...) }
func sealAD(denID []byte) []byte    { return append([]byte("joined_dens.dm_seal:"), denID...) }

var installSealAD = []byte("vault.dm_seal")

func loadJoined(ctx context.Context, db *sql.DB, v *vault.Vault) ([]*joined, error) {
	rows, err := db.QueryContext(ctx, `SELECT den_id, profile, device_key, joined_at, dm_seal FROM joined_dens ORDER BY joined_at`)
	if err != nil {
		return nil, fmt.Errorf("load joined dens: %w", err)
	}
	defer rows.Close()
	var out []*joined
	fail := func(err error) ([]*joined, error) {
		for _, j := range out {
			j.close()
		}
		return nil, err
	}
	for rows.Next() {
		var id, sealedProfile, sealedKey, sealedSeal []byte
		var at int64
		if err := rows.Scan(&id, &sealedProfile, &sealedKey, &at, &sealedSeal); err != nil {
			return fail(err)
		}
		plain, err := v.Open(sealedProfile, profileAD(id))
		if err != nil {
			return fail(fmt.Errorf("open joined den profile: %w", err))
		}
		var p Profile
		if err := json.Unmarshal(plain, &p); err != nil {
			return fail(fmt.Errorf("decode joined den profile: %w", err))
		}
		seed, err := v.Open(sealedKey, keyAD(id))
		if err != nil {
			return fail(fmt.Errorf("open device key: %w", err))
		}
		key, err := vault.NewSigningKey(seed)
		clear(seed)
		if err != nil {
			return fail(err)
		}
		j := &joined{denID: id, profile: p, key: key, joinedAt: time.UnixMilli(at)}
		out = append(out, j)
		if sealedSeal != nil {
			seal, err := v.Open(sealedSeal, sealAD(id))
			if err != nil {
				return fail(fmt.Errorf("open DM seal: %w", err))
			}
			j.seal, err = vault.NewSecret(seal)
			clear(seal)
			if err != nil {
				return fail(err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fail(err)
	}
	return out, nil
}

func sealProfile(v *vault.Vault, denID []byte, p Profile) ([]byte, error) {
	plain, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return v.Seal(plain, profileAD(denID))
}

func insertJoined(ctx context.Context, db *sql.DB, v *vault.Vault, j *joined) error {
	sealedProfile, err := sealProfile(v, j.denID, j.profile)
	if err != nil {
		return err
	}
	seed := j.key.Seed()
	sealedKey, err := v.Seal(seed, keyAD(j.denID))
	clear(seed)
	if err != nil {
		return err
	}
	var sealedSeal []byte
	if j.seal != nil {
		seal := j.seal.Copy()
		sealedSeal, err = v.Seal(seal, sealAD(j.denID))
		clear(seal)
		if err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `INSERT INTO joined_dens (den_id, profile, device_key, joined_at, dm_seal) VALUES (?, ?, ?, ?, ?)`,
		[]byte(j.denID), sealedProfile, sealedKey, j.joinedAt.UnixMilli(), sealedSeal)
	if err != nil {
		return fmt.Errorf("store joined den: %w", err)
	}
	return nil
}

// storeSeal keeps a den's DM seal.
func storeSeal(ctx context.Context, db *sql.DB, v *vault.Vault, denID, seal []byte) error {
	sealed, err := v.Seal(seal, sealAD(denID))
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE joined_dens SET dm_seal = ? WHERE den_id = ?`, sealed, denID); err != nil {
		return fmt.Errorf("store DM seal: %w", err)
	}
	return nil
}

// installSeal returns the seal this install starts new dens with, or nil
// before its first den has one.
func installSeal(ctx context.Context, db *sql.DB, v *vault.Vault) ([]byte, error) {
	var sealed []byte
	if err := db.QueryRowContext(ctx, `SELECT dm_seal FROM vault WHERE id = 1`).Scan(&sealed); err != nil {
		return nil, fmt.Errorf("read the install's DM seal: %w", err)
	}
	if sealed == nil {
		return nil, nil
	}
	seal, err := v.Open(sealed, installSealAD)
	if err != nil {
		return nil, fmt.Errorf("open the install's DM seal: %w", err)
	}
	return seal, nil
}

// setInstallSeal makes seal the one this install starts new dens with.
func setInstallSeal(ctx context.Context, db *sql.DB, v *vault.Vault, seal []byte) error {
	sealed, err := v.Seal(seal, installSealAD)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE vault SET dm_seal = ? WHERE id = 1`, sealed)
	if err != nil {
		return fmt.Errorf("store the install's DM seal: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("the vault isn't set up")
	}
	return nil
}

func updateProfile(ctx context.Context, db *sql.DB, v *vault.Vault, denID []byte, p Profile) error {
	sealed, err := sealProfile(v, denID, p)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE joined_dens SET profile = ? WHERE den_id = ?`, sealed, denID)
	return err
}
