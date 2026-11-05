package den

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Identities, signed DM keys and members' records (M16.1). The den keeps
// each member's identity as their Dens published it and hands it to any
// member who asks; it relays the keys of DMs sealed to their members'
// identities, each signed by its maker, with each member's copy; and it
// keeps each member's record, sealed with their seal. It can't open any
// of it. What it checks keeps honest clients in step: a key names its
// members' identities as published now and checks against its maker's,
// one live key per DM, and a record written over the revision it was
// merged from. Clients check the cryptography themselves and trust none
// of this.

var errRecordConflict = denproto.Errorf(http.StatusConflict, denproto.CodeEditConflict, "the record was written since that revision")

// PublishIdentity sets the session's member's identity, as their Dens
// publishes it as it signs in, where the den has none for them or holds
// another. A change reaches everyone as member.updated, and clients that
// kept the list of members get it with their next snapshot (M6.2).
func (d *Den) PublishIdentity(ctx context.Context, s *Session, id denproto.Identity) (denproto.Member, error) {
	if err := id.Check(); err != nil {
		return denproto.Member{}, invalid("identity: %v", err)
	}
	hash := id.Hash()
	now := d.now()
	changed := false
	// A key's checks read the identities, under the same lock.
	d.keys.Lock()
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var current []byte
		if err := tx.QueryRowContext(ctx, `SELECT identity FROM den_members WHERE id = ?`, s.MemberID).Scan(&current); err != nil {
			return err
		}
		if bytes.Equal(current, hash) {
			return nil
		}
		changed = true
		if _, err := tx.ExecContext(ctx, `UPDATE den_members SET identity = ? WHERE id = ?`, []byte(hash), s.MemberID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_identities (member_id, kem, sig) VALUES (?, ?, ?)
			ON CONFLICT (member_id) DO UPDATE SET kem = excluded.kem, sig = excluded.sig`, s.MemberID, []byte(id.KEM), []byte(id.Sig)); err != nil {
			return err
		}
		return logMember(ctx, tx, s.MemberID, now)
	})
	d.keys.Unlock()
	if err != nil {
		return denproto.Member{}, err
	}
	m, err := d.Member(ctx, s.MemberID)
	if err != nil || !changed {
		return m, err
	}
	d.log.Infof("Member %d published an identity", s.MemberID)
	return m, d.Hub.Publish(denproto.EventMemberUpdated, m, Everyone)
}

// Identities returns the identities of the members named, of those who
// have one, for a Dens that needs them: a few at a time, as it meets
// them. Any member may fetch anyone's, as they may anyone's profile.
func (d *Den) Identities(ctx context.Context, members []string) (denproto.IdentityList, error) {
	list := denproto.IdentityList{Identities: []denproto.MemberIdentity{}}
	if len(members) == 0 || len(members) > denproto.MaxIdentities {
		return list, invalid("members: 1 to %d", denproto.MaxIdentities)
	}
	ids := make([]any, len(members))
	for i, m := range members {
		id, err := denproto.ParseID(m)
		if err != nil {
			return list, invalid("members: %q isn't an ID", m)
		}
		ids[i] = id
	}
	rows, err := d.db.QueryContext(ctx, `SELECT member_id, kem, sig FROM den_identities
		WHERE member_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`) ORDER BY member_id`, ids...)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kem, sig []byte
		if err := rows.Scan(&id, &kem, &sig); err != nil {
			return list, err
		}
		list.Identities = append(list.Identities, denproto.MemberIdentity{MemberID: denproto.FormatID(id), KEM: kem, Sig: sig})
	}
	return list, rows.Err()
}

// keptStatement is a signed key's statement as the den keeps it, with its
// maker's keys as they were when they made it.
type keptStatement struct {
	KID     denproto.Bytes       `json:"kid"`
	Commit  denproto.Bytes       `json:"commit"`
	Members []denproto.KeyMember `json:"members"`
	Maker   denproto.KeyMaker    `json:"maker"`
}

// loadSignedKeys returns the signed keys a condition on den_dm_keys
// (aliased k) selects, oldest first, as member sees them, with their own
// copy.
func loadSignedKeys(ctx context.Context, q querier, member int64, where string, args ...any) ([]denproto.SignedKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT k.id, k.channel_id, k.started_by, k.started_at, k.statement, k.signature,
		k.retired_at, k.retired_by, k.retired,
		(SELECT m.sealed FROM den_dm_key_members m WHERE m.key_id = k.id AND m.member_id = ?)
		FROM den_dm_keys k WHERE k.statement IS NOT NULL AND `+where+` ORDER BY k.id`, append([]any{member}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []denproto.SignedKey{}
	for rows.Next() {
		var k denproto.SignedKey
		var id, channel, by int64
		var statement, signature, copied []byte
		var retiredAt, retiredBy sql.NullInt64
		var retired sql.NullString
		if err := rows.Scan(&id, &channel, &by, &k.MadeAt, &statement, &signature, &retiredAt, &retiredBy, &retired, &copied); err != nil {
			return nil, err
		}
		var kept keptStatement
		if err := json.Unmarshal(statement, &kept); err != nil {
			return nil, err
		}
		k.ID, k.ChannelID, k.MadeBy = denproto.FormatID(id), denproto.FormatID(channel), denproto.FormatID(by)
		k.KID, k.Commit, k.Members, k.Maker, k.Signature, k.Copy = kept.KID, kept.Commit, kept.Members, kept.Maker, signature, copied
		if retired.Valid {
			k.RetiredAt, k.RetiredBy, k.Retired = retiredAt.Int64, denproto.FormatID(retiredBy.Int64), retired.String
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// memberSignedKeys lists the signed keys of every DM a member is in, for
// their snapshot, and with groups those of the group DMs they're in that
// they hold a copy of (M16.3).
func memberSignedKeys(ctx context.Context, q querier, member int64, groups bool) ([]denproto.SignedKey, error) {
	return loadSignedKeys(ctx, q, member, `(k.channel_id IN (SELECT id FROM den_channels
		WHERE kind = 'dm' AND (dm_low = ?2 OR dm_high = ?2)) OR ?3 AND k.channel_id IN (SELECT channel_id FROM den_group_members WHERE member_id = ?2)
		AND EXISTS (SELECT 1 FROM den_dm_key_members c WHERE c.key_id = k.id AND c.member_id = ?2))`, member, groups)
}

// SignedKeys lists a DM's signed keys, oldest first, as the session's
// member sees them, or a group's they hold a copy of (M16.3).
func (d *Den) SignedKeys(ctx context.Context, s *Session, channelID string) (denproto.SignedKeyList, error) {
	_, cid, _, err := d.keyChannel(ctx, s, channelID, false)
	if err != nil {
		return denproto.SignedKeyList{}, err
	}
	keys, err := loadSignedKeys(ctx, d.db, s.MemberID, `k.channel_id = ?2 AND ((SELECT kind FROM den_channels WHERE id = ?2) = 'dm'
		OR EXISTS (SELECT 1 FROM den_dm_key_members c WHERE c.key_id = k.id AND c.member_id = ?3))`, cid, s.MemberID)
	return denproto.SignedKeyList{Keys: keys}, err
}

// published is a member's identity as published now: its hash and keys.
type published struct {
	hash, kem, sig []byte
}

func publishedIdentity(ctx context.Context, q querier, member int64) (published, bool, error) {
	var p published
	err := q.QueryRowContext(ctx, `SELECT m.identity, i.kem, i.sig FROM den_members m LEFT JOIN den_identities i ON i.member_id = m.id
		WHERE m.id = ?`, member).Scan(&p.hash, &p.kem, &p.sig)
	if err != nil {
		return p, false, err
	}
	return p, p.hash != nil && p.kem != nil, nil
}

// MakeSignedKey makes a DM's key from its maker's statement and a copy
// for each member, sealed to their identities, or a group DM's for its
// members as they are (M16.3). A DM has one live key: a key both members
// checked before M16.1 stays live, as does a signed key that names the
// identities its members have now, and a second made at once gets 409
// key_exists, so the slower takes the live one. A signed key that names an
// identity its member no longer has, or a group's members as they were,
// retires as replaced.
func (d *Den) MakeSignedKey(ctx context.Context, s *Session, channelID string, req denproto.SignedKeyRequest) (denproto.SignedKey, error) {
	c, cid, members, err := d.keyChannel(ctx, s, channelID, true)
	if err != nil {
		return denproto.SignedKey{}, err
	}
	group := c.Kind == denproto.KindGroupDM
	info, _ := d.Info()
	d.keys.Lock()
	defer d.keys.Unlock()
	now := d.now().UnixMilli()
	var id int64
	var retired []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if group {
			// The group's members as they are now, which a change may have
			// moved on since its channel was read.
			g, err := loadGroup(ctx, tx, info.ID, cid)
			if err != nil {
				return err
			}
			if !slices.Contains(g.members, s.MemberID) {
				return errChannelNotFound
			}
			members = g.members
		}
		var err error
		id, retired, err = putKey(ctx, tx, info.ID, cid, s.MemberID, members, req, now, false, group)
		return err
	})
	if err != nil {
		return denproto.SignedKey{}, err
	}
	d.log.Infof("Member %d made key %d for DM %d", s.MemberID, id, cid)
	if err := d.publishKeys(ctx, append(retired, id)...); err != nil {
		return denproto.SignedKey{}, err
	}
	if group && req.Name != nil {
		// Its name, sealed again under the new key.
		if c, err := d.channel(ctx, d.db, cid); err != nil {
			return denproto.SignedKey{}, err
		} else if err := d.Hub.Publish(denproto.EventChannelUpdated, c, audienceOfGroup(c)); err != nil {
			return denproto.SignedKey{}, err
		}
	}
	keys, err := loadSignedKeys(ctx, d.db, s.MemberID, `k.id = ?2`, id)
	if err != nil {
		return denproto.SignedKey{}, err
	}
	if len(keys) != 1 {
		return denproto.SignedKey{}, errKeyNotFound
	}
	return keys[0], nil
}

// putKey makes a signed key for a DM's or a group's members within tx,
// which its maker's statement names, each with the identity they publish
// now, with a copy for each, and the maker's signature; the caller holds
// d.keys. A live key both members checked before M16.1 stays (key_exists),
// as does a live signed key naming these members with the identities they
// publish now, unless replace says a change to a group made its
// membership new; any other retires as replaced. A group's key may carry
// the group's name sealed under it. It returns the key's ID and those it
// retired.
func putKey(ctx context.Context, tx *sql.Tx, denID []byte, cid, maker int64, members []int64, req denproto.SignedKeyRequest,
	now int64, replace, group bool) (int64, []int64, error) {
	stmt, err := req.Statement(denID, denproto.FormatID(cid), denproto.FormatID(maker))
	if err != nil {
		return 0, nil, invalid("statement: %v", err)
	}
	if len(stmt.Members) != len(members) || slices.ContainsFunc(members, func(m int64) bool { return stmt.Named(m) == nil }) {
		return 0, nil, invalid("members: a key names its DM's members")
	}
	if len(req.Copies) != len(req.Members) {
		return 0, nil, invalid("copies: one for each member")
	}
	copies := map[int64][]byte{}
	for _, cp := range req.Copies {
		id, err := denproto.ParseID(cp.MemberID)
		if err != nil || stmt.Named(id) == nil || copies[id] != nil || len(cp.Copy) != denproto.KeyCopySize {
			return 0, nil, invalid("copies: one of %d bytes for each member", denproto.KeyCopySize)
		}
		copies[id] = cp.Copy
	}
	if req.Name != nil && (!group || req.Name.Check() != nil || !denproto.Equal(req.Name.KID, req.KID)) {
		return 0, nil, invalid("name: a group's name, sealed under its key")
	}
	ids := map[int64]published{}
	for _, m := range members {
		p, ok, err := publishedIdentity(ctx, tx, m)
		if err != nil {
			return 0, nil, err
		}
		if !ok {
			return 0, nil, invalid("members: member %d has no identity", m)
		}
		if !bytes.Equal(p.hash, stmt.Named(m)) {
			return 0, nil, invalid("members: the key names another identity than member %d's", m)
		}
		ids[m] = p
	}
	made := ids[maker]
	if !denproto.VerifyStatement(made.sig, stmt, req.Signature) {
		return 0, nil, invalid("signature: it doesn't check against your identity")
	}
	live, err := dmLiveKey(ctx, tx, cid)
	if err != nil {
		return 0, nil, err
	}
	var retired []int64
	if live.ok {
		var statement []byte
		if err := tx.QueryRowContext(ctx, `SELECT statement FROM den_dm_keys WHERE id = ?`, live.id).Scan(&statement); err != nil {
			return 0, nil, err
		}
		if statement == nil && !replace {
			return 0, nil, errKeyExists // checked by both members before M16.1
		}
		var kept keptStatement
		if statement != nil {
			if err := json.Unmarshal(statement, &kept); err != nil {
				return 0, nil, err
			}
		}
		current := len(kept.Members) == len(members) && !slices.ContainsFunc(kept.Members, func(m denproto.KeyMember) bool {
			mid, _ := denproto.ParseID(m.MemberID)
			p, in := ids[mid]
			return !in || !bytes.Equal(m.Identity, p.hash)
		})
		if statement != nil && current && !replace {
			return 0, nil, errKeyExists
		}
		if retired, err = retireKeys(ctx, tx, maker, now, denproto.RetiredReplaced, `k.id = ?`, live.id); err != nil {
			return 0, nil, err
		}
	}
	statement, err := json.Marshal(keptStatement{KID: req.KID, Commit: req.Commit, Members: req.Members,
		Maker: denproto.KeyMaker{KEMHash: denproto.KEMHash(made.kem), Sig: made.sig}})
	if err != nil {
		return 0, nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO den_dm_keys (channel_id, started_by, started_at, stage, statement, signature)
		VALUES (?, ?, ?, 'revealed', ?, ?)`, cid, maker, now, statement, []byte(req.Signature))
	if err != nil {
		return 0, nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, nil, err
	}
	for member, copied := range copies {
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_dm_key_members (key_id, member_id, sealed, checked_at) VALUES (?, ?, ?, ?)`,
			id, member, copied, now); err != nil {
			return 0, nil, err
		}
	}
	if req.Name != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE den_group_dms SET name = ?, name_kid = ? WHERE channel_id = ?`,
			[]byte(req.Name.Sealed), []byte(req.Name.KID), cid); err != nil {
			return 0, nil, err
		}
	}
	return id, retired, nil
}

// Record returns the member's record, at revision 0 with nothing sealed
// before their first write.
func (d *Den) Record(ctx context.Context, s *Session) (denproto.Record, error) {
	var r denproto.Record
	var sealed []byte
	err := d.db.QueryRowContext(ctx, `SELECT revision, sealed FROM den_records WHERE member_id = ?`, s.MemberID).Scan(&r.Revision, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	r.Sealed = sealed
	return r, err
}

// WriteRecord writes the member's record over the revision it was merged
// from, or refuses with edit_conflict once another device wrote since. The
// member's sessions hear of the new revision, so their other devices
// merge it at once.
func (d *Den) WriteRecord(ctx context.Context, s *Session, req denproto.RecordWrite) (denproto.Record, error) {
	if !denproto.CheckSealedRecord(req.Sealed) {
		return denproto.Record{}, invalid("sealed: a record is padded to 16, 64 or 256 KiB")
	}
	if req.Revision < 0 {
		return denproto.Record{}, invalid("revision: 0 or more")
	}
	now := d.now().UnixMilli()
	var next int64
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var current int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM den_records WHERE member_id = ?`, s.MemberID).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current != req.Revision {
			return errRecordConflict
		}
		next = current + 1
		_, err = tx.ExecContext(ctx, `INSERT INTO den_records (member_id, revision, sealed, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (member_id) DO UPDATE SET revision = excluded.revision, sealed = excluded.sealed, updated_at = excluded.updated_at`,
			s.MemberID, next, []byte(req.Sealed), now)
		return err
	})
	if err != nil {
		return denproto.Record{}, err
	}
	return denproto.Record{Revision: next}, d.Hub.Publish(denproto.EventRecordUpdated, denproto.RecordUpdated{Revision: next}, OnlyMember(s.MemberID))
}
