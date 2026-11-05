package cache

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Disappearing messages (M16.4): a DM's or a group's message sent under a
// time keeps when it goes, and the cache keeps that beside it. The cache
// lets it go then, with the files on it and the replies' quotes of it,
// whether the den can be reached or not, so a den offline shows none of
// it, and keeps out a copy past its time that a page or an event brings.
// The client hears which went, to take them off its pages and out of the
// notifications; the den deletes them at the same time, by its own clock.

// Gone is a message the cache let go of as its time came.
type Gone struct {
	DenID       []byte
	ID, Channel int64
}

// disappearing is the timer that lets messages go at their time, which
// gone hears of. Without gone, as in most tests, a message goes only by
// Disappear.
type disappearing struct {
	timer *time.Timer
	at    int64 // when the timer goes, in the den's milliseconds
	gone  func([]Gone)
}

// goneRetry is how long the timer waits after a pass that failed.
const goneRetry = time.Minute

// OnDisappear has the cache let messages go at their time from now on,
// those whose time passed while it was closed first, and tell gone which
// went, outside its lock.
func (s *Store) OnDisappear(gone func([]Gone)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dis.gone = gone
	s.planLocked()
}

// planLocked sets the timer for the next message to go. The caller holds
// s.mu.
func (s *Store) planLocked() {
	d := &s.dis
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.gone == nil || s.closed {
		return
	}
	var next sql.NullInt64
	if err := s.db.QueryRow(`SELECT min(expires) FROM messages WHERE expires IS NOT NULL`).Scan(&next); err != nil {
		s.log.Warnf("Plan letting disappearing messages go: %v", err)
		next = sql.NullInt64{Int64: s.now().Add(goneRetry).UnixMilli(), Valid: true}
	}
	if next.Valid {
		s.goAtLocked(next.Int64)
	}
}

// goAtLocked sets the timer to go at a time, in milliseconds. The caller
// holds s.mu.
func (s *Store) goAtLocked(at int64) {
	d := &s.dis
	if d.timer != nil {
		d.timer.Stop()
	}
	d.at = at
	d.timer = time.AfterFunc(max(time.UnixMilli(at).Sub(s.now()), 0), s.disappear)
}

// dueLocked has the timer go by at, when a message just kept goes, where
// it isn't set to go sooner. The message's transaction may not commit, so
// the timer may go early, and its pass find nothing due. The caller holds
// s.mu.
func (s *Store) dueLocked(at int64) {
	d := &s.dis
	if d.gone == nil || s.closed || (d.timer != nil && d.at <= at) {
		return
	}
	s.goAtLocked(at)
}

// disappear lets go of the messages whose time came, plans the next pass,
// and tells the client which went.
func (s *Store) disappear() {
	gone, err := s.Disappear()
	s.mu.Lock()
	fn := s.dis.gone
	switch {
	case s.closed:
	case err != nil:
		s.log.Warnf("Let disappearing messages go: %v", err)
		s.goAtLocked(s.now().Add(goneRetry).UnixMilli())
	default:
		s.planLocked()
	}
	s.mu.Unlock()
	if fn != nil && len(gone) > 0 {
		fn(gone)
	}
}

// Disappear lets go of every message whose time came, in every den, with
// the files on it, has the replies that quoted it quote nothing, and
// returns which went.
func (s *Store) Disappear() ([]Gone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT m.den, d.den_id, m.id, m.channel FROM messages m JOIN dens d ON d.den = m.den
		WHERE m.expires <= ? ORDER BY m.den, m.id`, s.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	byDen := map[int64][]Gone{}
	var dens []int64
	for rows.Next() {
		var den int64
		var g Gone
		if err := rows.Scan(&den, &g.DenID, &g.ID, &g.Channel); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := byDen[den]; !ok {
			dens = append(dens, den)
		}
		byDen[den] = append(byDen[den], g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []Gone
	for _, den := range dens {
		gone := byDen[den]
		if err := s.letGoLocked(den, gone); err != nil {
			return out, err
		}
		out = append(out, gone...)
	}
	return out, nil
}

// letGoLocked lets go of a den's messages, as a delete does each one. The
// caller holds s.mu.
func (s *Store) letGoLocked(den int64, gone []Gone) error {
	g, err := s.guardsLocked(den)
	if err != nil {
		return err
	}
	ids := make([]int64, len(gone))
	for i, m := range gone {
		ids[i] = m.ID
		g.forget(m.ID)
	}
	in := `(?` + strings.Repeat(", ?", len(ids)-1) + `)`
	if err := s.dropLocked(`den = ? AND message IN `+in, append([]any{den}, anys(ids)...)...); err != nil {
		return err
	}
	added := map[int64]int64{}
	err = s.txLocked(func(tx *sql.Tx) error {
		for _, m := range gone {
			var channel int64
			var body []byte
			err := tx.QueryRow(`DELETE FROM messages WHERE den = ? AND id = ? RETURNING channel, body`, den, m.ID).Scan(&channel, &body)
			switch {
			case err == nil:
				added[channel] -= int64(len(body))
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
			more, err := s.requoteLocked(tx, den, gone[0].DenID, m.ID, nil)
			for c, n := range more {
				added[c] += n
			}
			if err != nil {
				return err
			}
		}
		return s.countLocked(tx, den, added)
	})
	if err != nil {
		return err
	}
	return s.settleLocked(den, total(added))
}
