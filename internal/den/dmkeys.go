package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// DM keys (M1.7). The den relays each key's exchange between the DM's two
// members' clients, and keeps what they seal with their DM seals: each
// side's state while the exchange runs, and each member's copy of the key
// once they compared check codes. It can't open any of it. What it
// enforces keeps honest clients in step: one live key per DM, each
// exchange message from the right member in the right order, and messages
// only under a live key their sender checked. Clients check the
// cryptography themselves and trust none of this.

var (
	errKeyNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such DM key")
	errKeyExists   = denproto.Errorf(http.StatusConflict, denproto.CodeKeyExists, "the DM has a key, or an exchange under way")
	errKeyStage    = denproto.Errorf(http.StatusConflict, denproto.CodeKeyExists, "the key's exchange isn't at that step")
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

// dmKeyColumns reads den_dm_keys aliased k.
const dmKeyColumns = `k.id, k.channel_id, k.started_by, k.started_at, k.stage, k.offer, k.answer, k.reveal, k.retired_at, k.retired_by, k.retired`

// loadKeys returns the DM keys a condition on den_dm_keys (aliased k)
// selects, oldest first, as member sees them: with their own sealed copy
// and, when exchange is set, the exchange of any still running, with their
// own sealed state.
func loadKeys(ctx context.Context, q querier, member int64, exchange bool, where string, args ...any) ([]denproto.DMKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+dmKeyColumns+` FROM den_dm_keys k WHERE `+where+` ORDER BY k.id`, args...)
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
// each as they see them.
func (d *Den) publishKeys(ctx context.Context, ids ...int64) error {
	for _, id := range ids {
		var low, high int64
		if err := d.db.QueryRowContext(ctx, `SELECT c.dm_low, c.dm_high FROM den_dm_keys k JOIN den_channels c ON c.id = k.channel_id
			WHERE k.id = ?`, id).Scan(&low, &high); err != nil {
			return err
		}
		for _, member := range []int64{low, high} {
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

func checkState(state []byte) error {
	if len(state) == 0 || len(state) > denproto.MaxExchangeState {
		return invalid("state: 1 to %d bytes", denproto.MaxExchangeState)
	}
	return nil
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

// StartKey starts a key for a DM with the starter's offer and sealed
// state. A DM has one live key at a time: while one is being made, or
// only one member checked it, Restart retires it to start again; once both
// checked it, it stays until a member starts over.
func (d *Den) StartKey(ctx context.Context, s *Session, channelID string, req denproto.KeyStartRequest) (denproto.DMKey, error) {
	_, cid, _, err := d.keyDM(ctx, s, channelID, true)
	if err != nil {
		return denproto.DMKey{}, err
	}
	if err := req.Offer.Check(); err != nil {
		return denproto.DMKey{}, invalid("offer: %v", err)
	}
	if err := checkState(req.State); err != nil {
		return denproto.DMKey{}, err
	}
	offer, err := json.Marshal(req.Offer)
	if err != nil {
		return denproto.DMKey{}, err
	}
	d.keys.Lock()
	defer d.keys.Unlock()
	now := d.now().UnixMilli()
	var id int64
	var retired []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		live, err := dmLiveKey(ctx, tx, cid)
		if err != nil {
			return err
		}
		if live.ok {
			if !req.Restart || live.checks == 2 {
				return errKeyExists
			}
			if retired, err = retireKeys(ctx, tx, s.MemberID, now, denproto.RetiredRestarted, `k.id = ?`, live.id); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_dm_keys (channel_id, started_by, started_at, stage, offer) VALUES (?, ?, ?, 'offered', ?)`,
			cid, s.MemberID, now, offer)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO den_dm_key_members (key_id, member_id, state) VALUES (?, ?, ?)`, id, s.MemberID, []byte(req.State))
		return err
	})
	if err != nil {
		return denproto.DMKey{}, err
	}
	return d.keyChanged(ctx, s, id, retired...)
}

// keyChanged announces the keys that changed, and returns the key id as
// the session's member now sees it.
func (d *Den) keyChanged(ctx context.Context, s *Session, id int64, also ...int64) (denproto.DMKey, error) {
	if err := d.publishKeys(ctx, append(also, id)...); err != nil {
		return denproto.DMKey{}, err
	}
	keys, err := loadKeys(ctx, d.db, s.MemberID, true, `k.id = ?`, id)
	if err != nil {
		return denproto.DMKey{}, err
	}
	if len(keys) != 1 {
		return denproto.DMKey{}, errKeyNotFound
	}
	return keys[0], nil
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

// AnswerKey answers a DM key's offer, from the member who didn't start it,
// with their sealed state.
func (d *Den) AnswerKey(ctx context.Context, s *Session, channelID, keyID string, req denproto.KeyAnswerRequest) (denproto.DMKey, error) {
	_, cid, _, err := d.keyDM(ctx, s, channelID, true)
	if err != nil {
		return denproto.DMKey{}, err
	}
	if err := req.Answer.Check(); err != nil {
		return denproto.DMKey{}, invalid("answer: %v", err)
	}
	if err := checkState(req.State); err != nil {
		return denproto.DMKey{}, err
	}
	answer, err := json.Marshal(req.Answer)
	if err != nil {
		return denproto.DMKey{}, err
	}
	d.keys.Lock()
	defer d.keys.Unlock()
	var id int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		live, err := dmKey(ctx, tx, cid, keyID)
		if err != nil {
			return err
		}
		if live.startedBy == s.MemberID {
			return forbidden("the member who didn't start the exchange answers it")
		}
		if live.stage != denproto.StageOffered {
			return errKeyStage
		}
		id = live.id
		if _, err := tx.ExecContext(ctx, `UPDATE den_dm_keys SET stage = 'answered', answer = ? WHERE id = ?`, answer, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO den_dm_key_members (key_id, member_id, state) VALUES (?, ?, ?)`, id, s.MemberID, []byte(req.State))
		return err
	})
	if err != nil {
		return denproto.DMKey{}, err
	}
	return d.keyChanged(ctx, s, id)
}

// RevealKey takes the starter's reveal, which must open their offer's
// commitment. Both sides can then work out the key and its check code.
func (d *Den) RevealKey(ctx context.Context, s *Session, channelID, keyID string, req denproto.KeyRevealRequest) (denproto.DMKey, error) {
	_, cid, other, err := d.keyDM(ctx, s, channelID, true)
	if err != nil {
		return denproto.DMKey{}, err
	}
	reveal, err := json.Marshal(req.Reveal)
	if err != nil {
		return denproto.DMKey{}, err
	}
	info, _ := d.Info()
	d.keys.Lock()
	defer d.keys.Unlock()
	var id int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		live, err := dmKey(ctx, tx, cid, keyID)
		if err != nil {
			return err
		}
		if live.startedBy != s.MemberID {
			return forbidden("the member who started the exchange reveals")
		}
		if live.stage != denproto.StageAnswered {
			return errKeyStage
		}
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT offer FROM den_dm_keys WHERE id = ?`, live.id).Scan(&raw); err != nil {
			return err
		}
		var offer denproto.ExchangeOffer
		if err := json.Unmarshal(raw, &offer); err != nil {
			return err
		}
		if !req.Reveal.Opens(denproto.DMExchangeContext(info.ID, cid, s.MemberID, other), offer) {
			return invalid("reveal: it doesn't open the offer's commitment")
		}
		id = live.id
		_, err = tx.ExecContext(ctx, `UPDATE den_dm_keys SET stage = 'revealed', reveal = ? WHERE id = ?`, reveal, id)
		return err
	})
	if err != nil {
		return denproto.DMKey{}, err
	}
	return d.keyChanged(ctx, s, id)
}

// SealKey stores the member's copy of a DM key, sealed with their seal,
// once they compared the check code. That's what lets them send with it,
// and their other devices read with it. Their state stays until the other
// member has checked too, so their Dens can still show the digits the
// other has yet to type; once both have, the exchange is done with and
// goes, states and all.
func (d *Den) SealKey(ctx context.Context, s *Session, channelID, keyID string, req denproto.KeySealRequest) (denproto.DMKey, error) {
	_, cid, _, err := d.keyDM(ctx, s, channelID, true)
	if err != nil {
		return denproto.DMKey{}, err
	}
	if len(req.Sealed) != denproto.SealedKeySize {
		return denproto.DMKey{}, invalid("sealed: a sealed key is %d bytes", denproto.SealedKeySize)
	}
	d.keys.Lock()
	defer d.keys.Unlock()
	now := d.now().UnixMilli()
	var id int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		live, err := dmKey(ctx, tx, cid, keyID)
		if err != nil {
			return err
		}
		if live.stage != denproto.StageRevealed {
			return errKeyStage
		}
		id = live.id
		res, err := tx.ExecContext(ctx, `UPDATE den_dm_key_members SET sealed = ?, checked_at = coalesce(checked_at, ?)
			WHERE key_id = ? AND member_id = ?`, []byte(req.Sealed), now, id, s.MemberID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errKeyNotFound
		}
		var both bool
		if err := tx.QueryRowContext(ctx, `SELECT count(*) = 2 FROM den_dm_key_members WHERE key_id = ? AND sealed IS NOT NULL`, id).Scan(&both); err != nil {
			return err
		}
		if !both {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_dm_keys SET offer = NULL, answer = NULL, reveal = NULL WHERE id = ?`, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE den_dm_key_members SET state = NULL WHERE key_id = ?`, id)
		return err
	})
	if err != nil {
		return denproto.DMKey{}, err
	}
	return d.keyChanged(ctx, s, id)
}

// sendKey checks the key a DM message is sealed with: the DM's live key,
// which its sender checked. Old keys stay for reading what they sealed.
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
		return 0, invalid("key_id: compare the check code before sending with it")
	}
	return live.id, nil
}

// resetSeal gives a member a new seal within tx: the live keys of their
// DMs retire, and their sealed copies and states go, since nothing opens
// them now. It returns the keys it retired.
func resetSeal(ctx context.Context, tx *sql.Tx, member int64, sealCheck []byte, now int64) ([]int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE den_members SET seal_check = ? WHERE id = ?`, sealCheck, member); err != nil {
		return nil, err
	}
	ids, err := retireKeys(ctx, tx, member, now, denproto.RetiredStartedOver, `k.channel_id IN (SELECT id FROM den_channels
		WHERE kind = 'dm' AND (dm_low = ? OR dm_high = ?))`, member, member)
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
	return denproto.StartedOver{SignedOut: len(gone)}, errors.Join(errs...)
}
