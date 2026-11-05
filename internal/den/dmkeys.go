package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// DM keys (M1.7). Before M16.1 the den relayed each key's exchange between
// the DM's two members' clients, and kept what they sealed with their DM
// seals: each side's state while the exchange ran, and each member's copy
// of the key once they compared check codes. From M16.1 it starts no
// exchange: a DM's new key is sealed to its members' identities (see
// identities.go), and the keys both members checked before stay live, with
// their copies. It can't open any of it. What it enforces keeps honest
// clients in step: one live key per DM, and messages only under a live key
// their sender holds a copy of. Clients check the cryptography themselves
// and trust none of this.

var (
	errKeyNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such DM key")
	errKeyExists   = denproto.Errorf(http.StatusConflict, denproto.CodeKeyExists, "the DM has a live key both members can use")
	errNoExchange  = denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField,
		"a DM's key is sealed to its members' identities now, and needs no exchange")
)

// keyDM returns a DM the session's member is in, with its other member,
// who must still be in the den for anything to change.
func (d *Den) keyDM(ctx context.Context, s *Session, channelID string, changing bool) (denproto.Channel, int64, int64, error) {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return c, 0, 0, err
	}
	if c.Kind != denproto.KindDM {
		return c, 0, 0, errChannelNotFound
	}
	other, ok := dmPartner(c, s.MemberID)
	if !ok {
		return c, 0, 0, errChannelNotFound
	}
	if changing {
		if err := d.checkDMOpen(ctx, c, s.MemberID); err != nil {
			return c, 0, 0, err
		}
	}
	return c, cid, other, nil
}

// keyChannel returns a DM or a group DM (M16.3) the session's member is
// in, with the members its keys are sealed to: a DM's two, the other of
// whom must still be in the den for anything to change, or a group's, as
// the den held them when it was read.
func (d *Den) keyChannel(ctx context.Context, s *Session, channelID string, changing bool) (denproto.Channel, int64, []int64, error) {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return c, 0, nil, err
	}
	if c.Kind == denproto.KindGroupDM {
		members := make([]int64, 0, len(c.Members))
		for _, m := range c.Members {
			id, _ := denproto.ParseID(m)
			members = append(members, id)
		}
		return c, cid, members, nil
	}
	c, cid, other, err := d.keyDM(ctx, s, channelID, changing)
	return c, cid, []int64{s.MemberID, other}, err
}

// dmKeyColumns reads den_dm_keys aliased k.
const dmKeyColumns = `k.id, k.channel_id, k.started_by, k.started_at, k.stage, k.offer, k.answer, k.reveal, k.retired_at, k.retired_by, k.retired`

// loadKeys returns the DM keys a condition on den_dm_keys (aliased k)
// selects, oldest first, as member sees them: with their own sealed copy
// and, when exchange is set, the exchange of any still running, with their
// own sealed state. They're M1.7's alone, never M16.1's signed keys, whose
// copies an older Dens would refuse as a protocol violation.
func loadKeys(ctx context.Context, q querier, member int64, exchange bool, where string, args ...any) ([]denproto.DMKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+dmKeyColumns+` FROM den_dm_keys k WHERE k.statement IS NULL AND `+where+` ORDER BY k.id`, args...)
	if err != nil {
		return nil, err
	}
	var keys []denproto.DMKey
	var ids []any
	byID := map[string]int{}
	for rows.Next() {
		var k denproto.DMKey
		var id, channel, by int64
		var offer, answer, reveal []byte
		var retiredAt, retiredBy sql.NullInt64
		var retired sql.NullString
		if err := rows.Scan(&id, &channel, &by, &k.StartedAt, &k.Stage, &offer, &answer, &reveal, &retiredAt, &retiredBy, &retired); err != nil {
			rows.Close()
			return nil, err
		}
		k.ID, k.ChannelID, k.StartedBy = denproto.FormatID(id), denproto.FormatID(channel), denproto.FormatID(by)
		if retired.Valid {
			k.RetiredAt, k.RetiredBy, k.Retired = retiredAt.Int64, denproto.FormatID(retiredBy.Int64), retired.String
		}
		if exchange && offer != nil {
			x := &denproto.DMExchange{}
			err := json.Unmarshal(offer, &x.Offer)
			if err == nil && answer != nil {
				err = json.Unmarshal(answer, &x.Answer)
			}
			if err == nil && reveal != nil {
				err = json.Unmarshal(reveal, &x.Reveal)
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
			k.Exchange = x
		}
		byID[k.ID] = len(keys)
		keys = append(keys, k)
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(keys) == 0 {
		return keys, err
	}
	rows, err = q.QueryContext(ctx, `SELECT key_id, member_id, state, sealed, checked_at FROM den_dm_key_members
		WHERE key_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`) ORDER BY key_id, checked_at, member_id`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, who int64
		var state, sealed []byte
		var checked sql.NullInt64
		if err := rows.Scan(&key, &who, &state, &sealed, &checked); err != nil {
			return nil, err
		}
		k := &keys[byID[denproto.FormatID(key)]]
		if checked.Valid {
			k.Checks = append(k.Checks, denproto.DMKeyCheck{MemberID: denproto.FormatID(who), At: checked.Int64})
		}
		if who == member {
			k.Sealed = sealed
			if k.Exchange != nil {
				k.Exchange.State = state
			}
		}
	}
	return keys, rows.Err()
}

// DMKeys lists a DM's keys, oldest first, as the session's member sees
// them, with the exchange of a key still being made.
func (d *Den) DMKeys(ctx context.Context, s *Session, channelID string) (denproto.DMKeyList, error) {
	_, cid, _, err := d.keyDM(ctx, s, channelID, false)
	if err != nil {
		return denproto.DMKeyList{}, err
	}
	keys, err := loadKeys(ctx, d.db, s.MemberID, true, `k.channel_id = ?`, cid)
	if keys == nil {
		keys = []denproto.DMKey{}
	}
	return denproto.DMKeyList{Keys: keys}, err
}

// memberKeys lists the keys of every DM a member is in, without their
// exchanges, for their snapshot.
func memberKeys(ctx context.Context, q querier, member int64) ([]denproto.DMKey, error) {
	keys, err := loadKeys(ctx, q, member, false, `k.channel_id IN (SELECT id FROM den_channels
		WHERE kind = 'dm' AND (dm_low = ?1 OR dm_high = ?1))`, member)
	if keys == nil {
		keys = []denproto.DMKey{}
	}
	return keys, err
}

// publishKeys tells each member of the keys' DMs how the keys now stand,
// each as they see them: M1.7's as dm.key, and signed keys as
// dm.signed_key (M16.1). A group's key (M16.3) goes to those it was sealed
// to who are in the group now, never to a socket speaking version 1: one
// removed has lost the group, whose keys mean nothing to them.
func (d *Den) publishKeys(ctx context.Context, ids ...int64) error {
	for _, id := range ids {
		var low, high sql.NullInt64
		var signed bool
		var kind string
		if err := d.db.QueryRowContext(ctx, `SELECT c.kind, c.dm_low, c.dm_high, k.statement IS NOT NULL FROM den_dm_keys k
			JOIN den_channels c ON c.id = k.channel_id WHERE k.id = ?`, id).Scan(&kind, &low, &high, &signed); err != nil {
			return err
		}
		members := []int64{low.Int64, high.Int64}
		if kind == denproto.KindGroupDM {
			var err error
			if members, err = keyMembers(ctx, d.db, id); err != nil {
				return err
			}
		}
		for _, member := range members {
			if signed {
				keys, err := loadSignedKeys(ctx, d.db, member, `k.id = ?2`, id)
				if err != nil {
					return err
				}
				audience := OnlyMember(member)
				if kind == denproto.KindGroupDM {
					audience = audience.aboutGroups()
				}
				if len(keys) == 1 {
					if err := d.Hub.Publish(denproto.EventDMSignedKey, keys[0], audience); err != nil {
						return err
					}
				}
				continue
			}
			keys, err := loadKeys(ctx, d.db, member, true, `k.id = ?`, id)
			if err != nil {
				return err
			}
			if len(keys) == 1 {
				if err := d.Hub.Publish(denproto.EventDMKey, keys[0], OnlyMember(member)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// keyMembers lists the members a group's key was sealed to, who hold a
// copy of it, and are in its group now.
func keyMembers(ctx context.Context, q querier, key int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.member_id FROM den_dm_key_members c JOIN den_dm_keys k ON k.id = c.key_id
		JOIN den_group_members g ON g.channel_id = k.channel_id AND g.member_id = c.member_id WHERE c.key_id = ? ORDER BY c.member_id`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var m int64
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// liveKey is a DM's live key: its ID, stage, who started it, and how many
// members checked it. ok is false when the DM has none.
type liveKey struct {
	id, startedBy int64
	stage         string
	checks        int
	ok            bool
}

func dmLiveKey(ctx context.Context, q querier, channel int64) (liveKey, error) {
	var k liveKey
	err := q.QueryRowContext(ctx, `SELECT k.id, k.started_by, k.stage,
		(SELECT count(*) FROM den_dm_key_members m WHERE m.key_id = k.id AND m.sealed IS NOT NULL)
		FROM den_dm_keys k WHERE k.channel_id = ? AND k.retired IS NULL`, channel).Scan(&k.id, &k.startedBy, &k.stage, &k.checks)
	if errors.Is(err, sql.ErrNoRows) {
		return k, nil
	}
	k.ok = err == nil
	return k, err
}

// retireKeys retires the live keys a condition on den_dm_keys (aliased k)
// selects, for why, by member: they take no new messages. Their exchanges
// stop, so their states go too. It returns the keys it retired.
func retireKeys(ctx context.Context, tx *sql.Tx, member, now int64, why, where string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `UPDATE den_dm_keys AS k SET retired_at = ?, retired_by = ?, retired = ?, offer = NULL, answer = NULL, reveal = NULL
		WHERE k.retired IS NULL AND `+where+` RETURNING id`, append([]any{now, member, why}, args...)...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE den_dm_key_members SET state = NULL WHERE key_id = ?`, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// StartKey refused to start an exchange from M16.1 on: a DM's new key is
// sealed to its members' identities, so an older Dens can't start a key a
// newer one couldn't use.
func (d *Den) StartKey(ctx context.Context, s *Session, channelID string, req denproto.KeyStartRequest) (denproto.DMKey, error) {
	if _, _, _, err := d.keyDM(ctx, s, channelID, true); err != nil {
		return denproto.DMKey{}, err
	}
	return denproto.DMKey{}, errNoExchange
}

// dmKey finds the DM's live key with an ID.
func dmKey(ctx context.Context, q querier, channel int64, keyID string) (liveKey, error) {
	live, err := dmLiveKey(ctx, q, channel)
	if err != nil {
		return live, err
	}
	id, perr := denproto.ParseID(keyID)
	if perr != nil || !live.ok || live.id != id {
		return live, errKeyNotFound
	}
	return live, nil
}

// sendKey checks the key a DM message is sealed with: the DM's live key,
// which its sender holds a copy of, as a member who checked an M1.7 key
// does, and both members of a signed key do. Old keys stay for reading
// what they sealed.
func sendKey(ctx context.Context, q querier, channel int64, keyID string, member int64) (int64, error) {
	live, err := dmKey(ctx, q, channel, keyID)
	if errors.Is(err, errKeyNotFound) {
		return 0, invalid("key_id: not the DM's live key")
	}
	if err != nil {
		return 0, err
	}
	var checked bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_dm_key_members WHERE key_id = ? AND member_id = ? AND sealed IS NOT NULL)`,
		live.id, member).Scan(&checked); err != nil {
		return 0, err
	}
	if !checked {
		return 0, invalid("key_id: a key the sender holds no copy of")
	}
	return live.id, nil
}

// resetSeal gives a member a new seal within tx: the live keys of their
// DMs retire, and their sealed copies and states go, since nothing opens
// them now; so do their identity, which came from the old seal, and their
// record, which it sealed (M16.1). Their Dens publishes the new identity
// as it signs in. It returns the keys it retired.
func resetSeal(ctx context.Context, tx *sql.Tx, member int64, sealCheck []byte, now int64) ([]int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE den_members SET seal_check = ?, identity = NULL WHERE id = ?`, sealCheck, member); err != nil {
		return nil, err
	}
	for _, stmt := range []string{`DELETE FROM den_identities WHERE member_id = ?`, `DELETE FROM den_records WHERE member_id = ?`} {
		if _, err := tx.ExecContext(ctx, stmt, member); err != nil {
			return nil, err
		}
	}
	if err := logMember(ctx, tx, member, time.UnixMilli(now)); err != nil {
		return nil, err
	}
	// Their groups' keys too (M16.3), whose next is sealed to the new
	// identity.
	ids, err := retireKeys(ctx, tx, member, now, denproto.RetiredStartedOver, `k.channel_id IN (SELECT id FROM den_channels
		WHERE kind = 'dm' AND (dm_low = ?4 OR dm_high = ?4)) OR k.channel_id IN (SELECT channel_id FROM den_group_members WHERE member_id = ?4)`, member)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM den_dm_key_members WHERE member_id = ?`, member)
	return ids, err
}

// StartOver gives the member a new DM seal, for when they've lost theirs
// and every device that held it, or to shut out a stolen one. Their DMs'
// keys retire, and each needs a new exchange and check before anything
// more is sent in it; what the old keys sealed stays unreadable to them.
// Every other device of theirs is signed out, since it holds the old seal.
// It takes the password, so a stolen session can't do it.
func (d *Den) StartOver(ctx context.Context, s *Session, req denproto.StartOverRequest) (denproto.StartedOver, error) {
	if err := denproto.Size("seal_check", req.SealCheck, denproto.SealCheckSize); err != nil {
		return denproto.StartedOver{}, err
	}
	d.keys.Lock()
	defer d.keys.Unlock()
	now := d.now().UnixMilli()
	var retired []int64
	var gone [][]byte
	err := d.tx(ctx, func(tx *sql.Tx) error {
		if err := d.checkPassword(ctx, tx, s, req.Verifier, ""); err != nil {
			return err
		}
		var err error
		if retired, err = resetSeal(ctx, tx, s.MemberID, req.SealCheck, now); err != nil {
			return err
		}
		gone, err = signOutOthers(ctx, tx, s.MemberID, s.KeyID)
		return err
	})
	if err != nil {
		return denproto.StartedOver{}, err
	}
	d.log.Infof("Member %d started over with a new DM seal, which signed out %d devices", s.MemberID, len(gone))
	d.cancelRequests(s.MemberID)
	errs := []error{d.devicesGone(s.MemberID, gone, denproto.CloseReasonStartedOver), d.publishKeys(ctx, retired...)}
	// Their identity went with the old seal.
	if m, err := d.Member(ctx, s.MemberID); err != nil {
		errs = append(errs, err)
	} else {
		errs = append(errs, d.Hub.Publish(denproto.EventMemberUpdated, m, Everyone))
	}
	return denproto.StartedOver{SignedOut: len(gone)}, errors.Join(errs...)
}
