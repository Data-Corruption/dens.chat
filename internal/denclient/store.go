package denclient

import (
	"context"
	"database/sql"
	"encoding/json"
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
}

func profileAD(denID []byte) []byte { return append([]byte("joined_dens.profile:"), denID...) }
func keyAD(denID []byte) []byte     { return append([]byte("joined_dens.device_key:"), denID...) }

func loadJoined(ctx context.Context, db *sql.DB, v *vault.Vault) ([]*joined, error) {
	rows, err := db.QueryContext(ctx, `SELECT den_id, profile, device_key, joined_at FROM joined_dens ORDER BY joined_at`)
	if err != nil {
		return nil, fmt.Errorf("load joined dens: %w", err)
	}
	defer rows.Close()
	var out []*joined
	fail := func(err error) ([]*joined, error) {
		for _, j := range out {
			j.key.Close()
		}
		return nil, err
	}
	for rows.Next() {
		var id, sealedProfile, sealedKey []byte
		var at int64
		if err := rows.Scan(&id, &sealedProfile, &sealedKey, &at); err != nil {
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
		out = append(out, &joined{denID: id, profile: p, key: key, joinedAt: time.UnixMilli(at)})
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
	_, err = db.ExecContext(ctx, `INSERT INTO joined_dens (den_id, profile, device_key, joined_at) VALUES (?, ?, ?, ?)`,
		[]byte(j.denID), sealedProfile, sealedKey, j.joinedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("store joined den: %w", err)
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
