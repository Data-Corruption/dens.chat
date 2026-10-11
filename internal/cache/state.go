package cache

import (
	"bytes"
	"database/sql"
	"errors"
)

// A den's state as the client last had it (M6.2): what the page shows of
// the den while the client can't reach it, and its members, which a
// snapshot then sends only the changes to. Both are the client's JSON,
// sealed here.

func stateAD(denID []byte) []byte   { return append([]byte("dens-cache-state-v1"), denID...) }
func membersAD(denID []byte) []byte { return append([]byte("dens-cache-members-v1"), denID...) }

// SaveState keeps a den's state.
func (s *Store) SaveState(denID []byte, state []byte) error {
	sealed, err := s.v.Seal(state, stateAD(denID))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	den, err := s.denLocked(denID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE dens SET state = ? WHERE den = ?`, sealed, den)
	return err
}

// State returns a den's state as it was kept, or nil without one.
func (s *Store) State(denID []byte) ([]byte, error) {
	s.mu.Lock()
	var sealed []byte
	err := s.db.QueryRow(`SELECT state FROM dens WHERE den_id = ?`, denID).Scan(&sealed)
	s.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) || err == nil && sealed == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.v.Open(sealed, stateAD(denID))
}

// Members is a den's list of members as it was kept, and the epoch and
// version it was current as of.
type Members struct {
	List    []byte
	Epoch   []byte
	Version int64
}

// SaveMembers keeps a den's members, current as of a snapshot's epoch and
// version, and the events since.
func (s *Store) SaveMembers(denID []byte, m Members) error {
	sealed, err := s.v.Seal(m.List, membersAD(denID))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	den, err := s.denLocked(denID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE dens SET members = ?, members_epoch = ?, members_version = ? WHERE den = ?`,
		sealed, bytes.Clone(m.Epoch), m.Version, den)
	return err
}

// Members returns a den's members as they were kept, and false without
// them.
func (s *Store) Members(denID []byte) (Members, bool, error) {
	s.mu.Lock()
	var m Members
	var sealed []byte
	err := s.db.QueryRow(`SELECT members, members_epoch, members_version FROM dens WHERE den_id = ?`, denID).
		Scan(&sealed, &m.Epoch, &m.Version)
	s.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) || err == nil && (sealed == nil || m.Epoch == nil) {
		return Members{}, false, nil
	}
	if err != nil {
		return Members{}, false, err
	}
	if m.List, err = s.v.Open(sealed, membersAD(denID)); err != nil {
		return Members{}, false, err
	}
	return m, true, nil
}
