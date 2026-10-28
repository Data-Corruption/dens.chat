package cache

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Messages (M6.2): the cache keeps the messages it fetched from other dens
// and those that arrived while the client was connected, as the den sent
// them, a DM's still sealed. Beside them it keeps runs: ranges of IDs in a
// channel for which it holds every message the den has. IDs increase with
// time, so an old range can lose messages but never gain one.
//
// A run that reached the channel's newest message records the connection
// it did so in, its generation. While that connection lasts, every new
// message arrives as an event and joins the run, so it still reaches the
// newest, and the newest page comes from the cache.

// channelCap is the most messages the cache keeps of one channel; past it
// the oldest go, so a busy channel followed for years can't take the whole
// cache. Tests lower it.
var channelCap = 20_000

// deletedMemory is how many deleted messages' IDs the cache remembers of a
// den, so a page fetched before a delete doesn't bring one back.
const deletedMemory = 4096

// Run is a range of a channel's IDs for which the cache holds every
// message. First says nothing in the channel comes before Lo, and Gen is
// the connection in which the run reached the channel's newest message,
// or 0.
type Run struct {
	Lo, Hi int64
	First  bool
	Gen    int64
}

func messageAD(denID []byte, id int64) []byte {
	ad := append([]byte("dens-cache-message-v1"), denID...)
	return binary.BigEndian.AppendUint64(ad, uint64(id))
}

// guards are what keeps a page fetched before a change from undoing it:
// the IDs of messages deleted lately, and how far retention reached.
type guards struct {
	deleted map[int64]bool
	order   []int64
	expired int64
}

func (g *guards) forget(id int64) {
	if g.deleted[id] {
		return
	}
	g.deleted[id] = true
	g.order = append(g.order, id)
	if len(g.order) > deletedMemory {
		delete(g.deleted, g.order[0])
		g.order = g.order[1:]
	}
}

// guardsLocked returns a den's guards. The caller holds s.mu.
func (s *Store) guardsLocked(den int64) (*guards, error) {
	if g, ok := s.guards[den]; ok {
		return g, nil
	}
	g := &guards{deleted: map[int64]bool{}}
	if err := s.db.QueryRow(`SELECT expired FROM dens WHERE den = ?`, den).Scan(&g.expired); err != nil {
		return nil, err
	}
	s.guards[den] = g
	return g, nil
}

// txLocked runs fn in a transaction. The caller holds s.mu.
func (s *Store) txLocked(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// putLocked keeps messages of a den within tx: new ones, and newer
// revisions of those it holds. A message deleted lately, or one retention
// reached, stays out. It returns the bytes this added to each channel.
func (s *Store) putLocked(tx *sql.Tx, den int64, denID []byte, g *guards, ms []denproto.Message) (map[int64]int64, error) {
	added := map[int64]int64{}
	for _, m := range ms {
		id, err := denproto.ParseID(m.ID)
		if err != nil {
			continue
		}
		channel, _ := denproto.ParseID(m.ChannelID)
		if g.deleted[id] || id <= g.expired {
			continue
		}
		var revision int
		var old []byte
		err = tx.QueryRow(`SELECT revision, body FROM messages WHERE den = ? AND id = ?`, den, id).Scan(&revision, &old)
		switch {
		case err == nil && revision >= m.Revision:
			continue // a page from before an edit keeps the edit
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return nil, err
		}
		body, err := s.sealMessage(denID, id, m)
		if err != nil {
			return nil, err
		}
		var reply any
		if r, err := denproto.ParseID(m.ReplyTo); err == nil {
			reply = r
		}
		if _, err := tx.Exec(`INSERT INTO messages (den, id, channel, revision, reply_to, body) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (den, id) DO UPDATE SET revision = excluded.revision, body = excluded.body`,
			den, id, channel, m.Revision, reply, body); err != nil {
			return nil, err
		}
		added[channel] += int64(len(body) - len(old))
	}
	return added, nil
}

func (s *Store) sealMessage(denID []byte, id int64, m denproto.Message) ([]byte, error) {
	plain, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return s.v.Seal(plain, messageAD(denID, id))
}

func (s *Store) openMessage(denID []byte, id int64, body []byte) (denproto.Message, error) {
	var m denproto.Message
	plain, err := s.v.Open(body, messageAD(denID, id))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(plain, &m)
	return m, err
}

// countLocked adds bytes to the channels they went to, and marks them
// used now, within tx. The caller holds s.mu, and counts the store's
// total once tx commits.
func (s *Store) countLocked(tx *sql.Tx, den int64, added map[int64]int64) error {
	for channel, n := range added {
		if _, err := tx.Exec(`INSERT INTO channels (den, channel, bytes, used_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (den, channel) DO UPDATE SET bytes = bytes + excluded.bytes, used_at = excluded.used_at`,
			den, channel, n, s.now().UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

func total(added map[int64]int64) int64 {
	var n int64
	for _, v := range added {
		n += v
	}
	return n
}

// runsLocked returns a channel's runs, lowest first, within q.
func runsLocked(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, den, channel int64) ([]Run, error) {
	rows, err := q.Query(`SELECT lo, hi, first, gen FROM runs WHERE den = ? AND channel = ? ORDER BY lo`, den, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.Lo, &r.Hi, &r.First, &r.Gen); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRun keeps a page of a channel's messages, and that the cache now holds
// every message with an ID from lo to hi. first says nothing comes before
// lo. A gen other than 0 says nothing came after hi either, as of the page,
// in that connection: the run then takes in the runs above it, whose
// messages arrived as events since, and reaches the newest.
func (s *Store) AddRun(denID []byte, channel, lo, hi int64, first bool, gen int64, ms []denproto.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, err := s.denLocked(denID)
	if err != nil {
		return err
	}
	g, err := s.guardsLocked(den)
	if err != nil {
		return err
	}
	var added map[int64]int64
	err = s.txLocked(func(tx *sql.Tx) error {
		if added, err = s.putLocked(tx, den, denID, g, ms); err != nil {
			return err
		}
		if lo <= hi || gen != 0 {
			if err := mergeRun(tx, den, channel, Run{Lo: lo, Hi: max(hi, lo-1), First: first, Gen: gen}); err != nil {
				return err
			}
		}
		if err := s.countLocked(tx, den, added); err != nil {
			return err
		}
		return capLocked(tx, den, channel, added)
	})
	if err != nil {
		return err
	}
	return s.settleLocked(den, total(added))
}

// mergeRun adds a run to a channel's within tx, joining those it
// overlaps or touches, and with a gen, every run above it.
func mergeRun(tx *sql.Tx, den, channel int64, r Run) error {
	runs, err := runsLocked(tx, den, channel)
	if err != nil {
		return err
	}
	merged := r
	if merged.Hi < merged.Lo {
		// An empty range after an ID, which only a gen gives meaning:
		// nothing came after it, so the run that holds it reaches the
		// newest. Without one, nothing says what lies between.
		x := r.Lo - 1
		var at *Run
		for i := range runs {
			if runs[i].Lo <= x && x <= runs[i].Hi {
				at = &runs[i]
			}
		}
		if at == nil {
			return nil
		}
		merged = Run{Lo: at.Lo, Hi: at.Hi, First: at.First, Gen: r.Gen}
	}
	for _, o := range runs {
		touches := o.Lo <= merged.Hi+1 && o.Hi+1 >= merged.Lo
		above := r.Gen != 0 && o.Lo > merged.Hi
		if !touches && !above {
			continue
		}
		if o.Lo < merged.Lo || (o.Lo == merged.Lo && o.First) {
			merged.First = o.First || (o.Lo == merged.Lo && merged.First)
		}
		merged.Lo, merged.Hi = min(merged.Lo, o.Lo), max(merged.Hi, o.Hi)
		if r.Gen == 0 && o.Gen != 0 && o.Hi >= merged.Hi {
			merged.Gen = o.Gen // the run that reaches the newest still does
		}
		if _, err := tx.Exec(`DELETE FROM runs WHERE den = ? AND channel = ? AND lo = ?`, den, channel, o.Lo); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO runs (den, channel, lo, hi, first, gen) VALUES (?, ?, ?, ?, ?, ?)`,
		den, channel, merged.Lo, merged.Hi, merged.First, merged.Gen)
	return err
}

// capLocked keeps at most channelCap messages of a channel, letting the
// oldest go and moving its runs' starts past them.
func capLocked(tx *sql.Tx, den, channel int64, added map[int64]int64) error {
	if _, ok := added[channel]; !ok {
		return nil
	}
	var cut sql.NullInt64
	err := tx.QueryRow(`SELECT id FROM messages WHERE den = ? AND channel = ? ORDER BY id DESC LIMIT 1 OFFSET ?`,
		den, channel, channelCap).Scan(&cut)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !cut.Valid {
		return nil
	}
	if err != nil {
		return err
	}
	var freed int64
	if err := tx.QueryRow(`SELECT coalesce(sum(length(body)), 0) FROM messages WHERE den = ? AND channel = ? AND id <= ?`,
		den, channel, cut.Int64).Scan(&freed); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE den = ? AND channel = ? AND id <= ?`, den, channel, cut.Int64); err != nil {
		return err
	}
	added[channel] -= freed
	if _, err := tx.Exec(`UPDATE channels SET bytes = bytes - ? WHERE den = ? AND channel = ?`, freed, den, channel); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM runs WHERE den = ? AND channel = ? AND hi <= ?`, den, channel, cut.Int64); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE runs SET lo = ?, first = 0 WHERE den = ? AND channel = ? AND lo <= ?`, cut.Int64+1, den, channel, cut.Int64)
	return err
}

// settleLocked counts bytes a change added to the cache, and lets go of
// what was used longest ago if it no longer fits. The caller holds s.mu.
func (s *Store) settleLocked(den int64, added int64) error {
	s.used += added
	if err := s.evictLocked(); err != nil {
		s.log.Warnf("make room in the cache: %v", err)
	}
	return nil
}

// AppendTail keeps a message that arrived as an event, in a channel whose
// highest run reaches the newest in this connection: the run takes it in.
// A channel whose highest run doesn't, or that has none, starts a run with
// it, which reaches the newest from then on.
func (s *Store) AppendTail(denID []byte, m denproto.Message, gen int64) error {
	id, err := denproto.ParseID(m.ID)
	if err != nil {
		return nil
	}
	channel, _ := denproto.ParseID(m.ChannelID)
	s.mu.Lock()
	defer s.mu.Unlock()
	den, err := s.denLocked(denID)
	if err != nil {
		return err
	}
	g, err := s.guardsLocked(den)
	if err != nil {
		return err
	}
	var added map[int64]int64
	err = s.txLocked(func(tx *sql.Tx) error {
		if added, err = s.putLocked(tx, den, denID, g, []denproto.Message{m}); err != nil {
			return err
		}
		runs, err := runsLocked(tx, den, channel)
		if err != nil {
			return err
		}
		if n := len(runs); n > 0 && runs[n-1].Gen == gen && runs[n-1].Hi < id {
			if _, err := tx.Exec(`UPDATE runs SET hi = ? WHERE den = ? AND channel = ? AND lo = ?`, id, den, channel, runs[n-1].Lo); err != nil {
				return err
			}
		} else if n == 0 || runs[n-1].Hi < id {
			if err := mergeRun(tx, den, channel, Run{Lo: id, Hi: id, Gen: gen}); err != nil {
				return err
			}
		}
		if err := s.countLocked(tx, den, added); err != nil {
			return err
		}
		return capLocked(tx, den, channel, added)
	})
	if err != nil {
		return err
	}
	return s.settleLocked(den, total(added))
}

// UpdateMessage keeps a message's newer revision, if the cache holds the
// message, and has the replies to it quote it as it is now.
func (s *Store) UpdateMessage(denID []byte, m denproto.Message) error {
	id, err := denproto.ParseID(m.ID)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return err
	}
	g, err := s.guardsLocked(den)
	if err != nil {
		return err
	}
	var added map[int64]int64
	err = s.txLocked(func(tx *sql.Tx) error {
		var held bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM messages WHERE den = ? AND id = ?)`, den, id).Scan(&held); err != nil {
			return err
		}
		added = map[int64]int64{}
		if held {
			if added, err = s.putLocked(tx, den, denID, g, []denproto.Message{m}); err != nil {
				return err
			}
		}
		quote := quoteOf(m)
		more, err := s.requoteLocked(tx, den, denID, id, &quote)
		for channel, n := range more {
			added[channel] += n
		}
		if err != nil {
			return err
		}
		return s.countLocked(tx, den, added)
	})
	if err != nil {
		return err
	}
	return s.settleLocked(den, total(added))
}

// quoteOf is what a reply to m quotes, as the den builds it: the start of
// its text, or in a DM, the whole of it sealed, for the client to open.
func quoteOf(m denproto.Message) denproto.Reply {
	if m.Sealed != nil {
		return denproto.Reply{AuthorID: m.AuthorID, Sealed: m.Sealed, KeyID: m.KeyID, Nonce: m.Nonce, Revision: m.Revision}
	}
	return denproto.Reply{AuthorID: m.AuthorID, Text: denproto.Excerpt(m.Text)}
}

// requoteLocked has the cached replies to a message quote it, or nothing
// for a message that's gone, within tx.
func (s *Store) requoteLocked(tx *sql.Tx, den int64, denID []byte, original int64, quote *denproto.Reply) (map[int64]int64, error) {
	rows, err := tx.Query(`SELECT id, channel, body FROM messages WHERE den = ? AND reply_to = ?`, den, original)
	if err != nil {
		return nil, err
	}
	type reply struct {
		id, channel int64
		body        []byte
	}
	var replies []reply
	for rows.Next() {
		var r reply
		if err := rows.Scan(&r.id, &r.channel, &r.body); err != nil {
			rows.Close()
			return nil, err
		}
		replies = append(replies, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	added := map[int64]int64{}
	for _, r := range replies {
		m, err := s.openMessage(denID, r.id, r.body)
		if err != nil {
			return nil, err
		}
		m.Reply = quote
		body, err := s.sealMessage(denID, r.id, m)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE messages SET body = ? WHERE den = ? AND id = ?`, body, den, r.id); err != nil {
			return nil, err
		}
		added[r.channel] += int64(len(body) - len(r.body))
	}
	return added, nil
}

// DeleteMessage forgets a message for good, and the replies' quotes of
// it; a page fetched before the delete doesn't bring it back.
func (s *Store) DeleteMessage(denID []byte, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return err
	}
	g, err := s.guardsLocked(den)
	if err != nil {
		return err
	}
	g.forget(id)
	added := map[int64]int64{}
	err = s.txLocked(func(tx *sql.Tx) error {
		var channel int64
		var body []byte
		err := tx.QueryRow(`DELETE FROM messages WHERE den = ? AND id = ? RETURNING channel, body`, den, id).Scan(&channel, &body)
		switch {
		case err == nil:
			added[channel] -= int64(len(body))
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		more, err := s.requoteLocked(tx, den, denID, id, nil)
		for channel, n := range more {
			added[channel] += n
		}
		if err != nil {
			return err
		}
		return s.countLocked(tx, den, added)
	})
	if err != nil {
		return err
	}
	return s.settleLocked(den, total(added))
}

// Expire forgets every message up to through, in every channel of a den,
// as a retention pass deletes them, and the replies' quotes of them. A run
// that reached past through now starts the channel.
func (s *Store) Expire(denID []byte, through int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return err
	}
	g, err := s.guardsLocked(den)
	if err != nil {
		return err
	}
	if through <= g.expired {
		return nil
	}
	added := map[int64]int64{}
	err = s.txLocked(func(tx *sql.Tx) error {
		rows, err := tx.Query(`DELETE FROM messages WHERE den = ? AND id <= ? RETURNING channel, length(body)`, den, through)
		if err != nil {
			return err
		}
		for rows.Next() {
			var channel, n int64
			if err := rows.Scan(&channel, &n); err != nil {
				rows.Close()
				return err
			}
			added[channel] -= n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM runs WHERE den = ? AND hi <= ?`, den, through); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE runs SET lo = max(lo, ?), first = 1 WHERE den = ? AND lo <= ?`, through+1, den, through+1); err != nil {
			return err
		}
		// The replies that quoted them quote nothing now.
		rows, err = tx.Query(`SELECT DISTINCT reply_to FROM messages WHERE den = ? AND reply_to <= ?`, den, through)
		if err != nil {
			return err
		}
		var originals []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			originals = append(originals, id)
		}
		rows.Close()
		for _, id := range originals {
			more, err := s.requoteLocked(tx, den, denID, id, nil)
			if err != nil {
				return err
			}
			for channel, n := range more {
				added[channel] += n
			}
		}
		if _, err := tx.Exec(`UPDATE dens SET expired = max(expired, ?) WHERE den = ?`, through, den); err != nil {
			return err
		}
		for channel, n := range added {
			if _, err := tx.Exec(`UPDATE channels SET bytes = bytes + ? WHERE den = ? AND channel = ?`, n, den, channel); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	g.expired = through
	return s.settleLocked(den, total(added))
}

// Query selects a page of a channel's messages, as history does: the
// newest, or before, after or around an ID.
type Query struct {
	Before, After, Around int64
	Limit                 int
}

// Page is a page of a channel's messages, oldest first.
type Page struct {
	Messages []denproto.Message
	HasOlder bool
	HasNewer bool
}

// Page answers a query from the cache, and reports false when the cache
// can't: it holds no run that covers it. gen is the client's connection;
// with offline set, the client can't reach the den, and the highest run
// stands for the newest. empty says the channel has no messages, as far
// as the client knows.
func (s *Store) Page(denID []byte, channel int64, q Query, gen int64, offline, empty bool) (Page, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p Page
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return p, false, err
	}
	runs, err := runsLocked(s.db, den, channel)
	if err != nil {
		return p, false, err
	}
	n := q.Limit
	if n <= 0 {
		n = denproto.DefaultHistory
	}
	var tail *Run
	if len(runs) > 0 {
		r := runs[len(runs)-1]
		if offline || (gen != 0 && r.Gen == gen) {
			tail = &r
		}
	}
	// covering finds the run that holds id, or reaches the newest below it.
	covering := func(id int64) *Run {
		for i := range runs {
			r := &runs[i]
			if r.Lo <= id && (id <= r.Hi || (tail != nil && r.Lo == tail.Lo)) {
				return r
			}
		}
		return nil
	}
	older := func(r *Run, below int64, limit int) ([]denproto.Message, bool, error) {
		ms, err := s.rangeLocked(den, denID, channel, r.Lo, below-1, limit+1, true)
		more := len(ms) > limit
		if more {
			ms = ms[1:]
		}
		return ms, more || !r.First, err
	}
	newer := func(r *Run, from int64, limit int) ([]denproto.Message, bool, error) {
		hi := r.Hi
		if tail != nil && r.Lo == tail.Lo {
			hi = 1<<62 - 1
		}
		ms, err := s.rangeLocked(den, denID, channel, from, hi, limit+1, false)
		more := len(ms) > limit
		if more {
			ms = ms[:limit]
		}
		return ms, more || tail == nil || r.Lo != tail.Lo, err
	}
	switch {
	case q.Around > 0:
		r := covering(q.Around)
		if r == nil {
			return p, false, nil
		}
		before, hasOlder, err := older(r, q.Around, n/2)
		if err != nil {
			return p, false, err
		}
		after, hasNewer, err := newer(r, q.Around, n-n/2)
		if err != nil {
			return p, false, err
		}
		// Both sides come whole or not at all, as the den would send them.
		if (len(before) < n/2 && !r.First) || (len(after) < n-n/2 && hasNewer) {
			return p, false, nil
		}
		p = Page{Messages: append(before, after...), HasOlder: hasOlder, HasNewer: hasNewer}
	case q.Before > 0:
		r := covering(q.Before - 1)
		if r == nil {
			return p, false, nil
		}
		ms, hasOlder, err := older(r, q.Before, n)
		if err != nil || (len(ms) == 0 && !r.First) {
			return p, false, err
		}
		p = Page{Messages: ms, HasOlder: hasOlder, HasNewer: true}
		if tail != nil && r.Lo == tail.Lo {
			later, err := s.rangeLocked(den, denID, channel, q.Before, 1<<62-1, 1, false)
			if err != nil {
				return p, false, err
			}
			p.HasNewer = len(later) > 0
		}
	case q.After > 0:
		r := covering(q.After + 1)
		if r == nil {
			r = covering(q.After)
		}
		if r == nil {
			return p, false, nil
		}
		ms, hasNewer, err := newer(r, q.After+1, n)
		if err != nil || (len(ms) == 0 && hasNewer) {
			return p, false, err
		}
		p = Page{Messages: ms, HasNewer: hasNewer, HasOlder: true}
		if r.First {
			earlier, err := s.rangeLocked(den, denID, channel, r.Lo, q.After, 1, true)
			if err != nil {
				return p, false, err
			}
			p.HasOlder = len(earlier) > 0
		}
	default:
		if tail == nil {
			if len(runs) == 0 && empty && !offline {
				return Page{Messages: []denproto.Message{}}, true, nil
			}
			return p, false, nil
		}
		ms, hasOlder, err := older(tail, 1<<62-1, n)
		if err != nil {
			return p, false, err
		}
		// A run that lost every message it held to deletes says nothing
		// of what lies before it, unless it starts the channel: the den
		// answers, as for a page before. An empty page with older ones
		// would give the page no message to ask for them before.
		if len(ms) == 0 && !tail.First {
			return p, false, nil
		}
		p = Page{Messages: ms, HasOlder: hasOlder}
	}
	if p.Messages == nil {
		p.Messages = []denproto.Message{}
	}
	_, err = s.db.Exec(`UPDATE channels SET used_at = ? WHERE den = ? AND channel = ?`, s.now().UnixMilli(), den, channel)
	return p, true, err
}

// rangeLocked reads a channel's messages from lo to hi, at most limit,
// oldest first; with fromTop, the newest of them.
func (s *Store) rangeLocked(den int64, denID []byte, channel, lo, hi int64, limit int, fromTop bool) ([]denproto.Message, error) {
	order := "ASC"
	if fromTop {
		order = "DESC"
	}
	rows, err := s.db.Query(`SELECT id, body FROM messages WHERE den = ? AND channel = ? AND id BETWEEN ? AND ? ORDER BY id `+order+` LIMIT ?`,
		den, channel, lo, hi, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []denproto.Message
	for rows.Next() {
		var id int64
		var body []byte
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		m, err := s.openMessage(denID, id, body)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if fromTop {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, nil
}

// Runs lists a channel's runs, lowest first.
func (s *Store) Runs(denID []byte, channel int64) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return nil, err
	}
	return runsLocked(s.db, den, channel)
}

// Reach marks the channels whose highest run reaches the newest message,
// by the newest each holds now, as a snapshot says them, as reaching it
// in connection gen. A channel with none, or a run short of it, doesn't.
func (s *Store) Reach(denID []byte, newest map[int64]int64, gen int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return err
	}
	return s.txLocked(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT channel, max(hi) FROM runs WHERE den = ? GROUP BY channel`, den)
		if err != nil {
			return err
		}
		reach := map[int64]int64{}
		for rows.Next() {
			var channel, hi int64
			if err := rows.Scan(&channel, &hi); err != nil {
				rows.Close()
				return err
			}
			if last, ok := newest[channel]; ok && hi >= last {
				reach[channel] = hi
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for channel, hi := range reach {
			if _, err := tx.Exec(`UPDATE runs SET gen = ? WHERE den = ? AND channel = ? AND hi = ?`, gen, den, channel, hi); err != nil {
				return err
			}
		}
		return nil
	})
}

// Newest is the newest message the cache holds, or holds a file of, in a
// den, or 0: changes to messages newer than it can't touch the cache.
func (s *Store) Newest(denID []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return 0, err
	}
	var n sql.NullInt64
	err = s.db.QueryRow(`SELECT max(m) FROM (SELECT max(id) AS m FROM messages WHERE den = ?1 UNION ALL SELECT max(message) FROM files WHERE den = ?1)`,
		den).Scan(&n)
	return n.Int64, err
}

// dropMessagesLocked forgets a den's messages and runs, in the channels a
// condition selects, or all of them with none. The caller holds s.mu.
func (s *Store) dropMessagesLocked(den int64, where string, args ...any) error {
	cond := `den = ?`
	if where != "" {
		cond += ` AND (` + where + `)`
	}
	all := append([]any{den}, args...)
	var freed int64
	err := s.txLocked(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT coalesce(sum(bytes), 0) FROM channels WHERE `+cond, all...).Scan(&freed); err != nil {
			return err
		}
		for _, table := range []string{"messages", "runs", "channels"} {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE `+cond, all...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.used -= freed
	return nil
}

// channelsIn writes a list of channels as an IN clause's placeholders.
func channelsIn(channels []int64) string {
	return `(?` + strings.Repeat(", ?", len(channels)-1) + `)`
}
