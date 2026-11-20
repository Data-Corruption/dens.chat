package den

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Call records: each voice channel keeps its longest call's
// length, from the call's first join to its last member's leaving, held
// calls counting. The den writes it as a call ends longer than it, and
// each minute a call runs past it, so a den that stops without ending its
// calls loses a minute of one at most; it keeps the length alone, not who
// was in the call or when. Each call's state carries its length and the
// record as it began, which pages show it against. Staff reset a record,
// and a call going on then writes none, so a call left running can't set
// it again as it ends.

// recordEvery is how often a call past its channel's record writes its
// length.
const recordEvery = time.Minute

// recordWrite is a record decided under the registry's lock, to write
// once it's let go: a channel's longest call, or with ms 0, none, as staff
// reset it.
type recordWrite struct {
	channel int64
	ms      int64
}

// storedRecord reads a voice channel's record as the database holds it, 0
// for none.
func storedRecord(ctx context.Context, q querier, cid int64) (int64, error) {
	var ms sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT call_record FROM den_channels WHERE id = ?`, cid).Scan(&ms)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errChannelNotFound
	}
	return ms.Int64, err
}

// nextMark is when the call's length next reaches a whole minute past
// what the den kept of its channel's record, or zero once it writes none.
// It's a whole millisecond, as the den's timer counts, so the timer never
// goes a moment before it.
func (room *callRoom) nextMark() time.Time {
	if !room.counts {
		return time.Time{}
	}
	minutes := room.kept/recordEvery.Milliseconds() + 1
	mark := room.began.Add(time.Duration(minutes) * recordEvery)
	return mark.Add(time.Millisecond - 1).Truncate(time.Millisecond)
}

// recordLocked decides to write a call's length as its channel's record,
// as of now, if it's longer than what the den kept. The caller holds the
// registry's lock, and lets go of it with unlockCalls.
func (d *Den) recordLocked(cid int64, room *callRoom, now time.Time) {
	length := now.Sub(room.began).Milliseconds()
	if !room.counts || length <= room.kept {
		return
	}
	r := &d.calls
	room.kept = length
	r.records[cid] = length
	r.writes = append(r.writes, recordWrite{cid, length})
}

// unlockCalls lets go of the registry's lock, then writes the records
// decided under it, and returns the error of a reset among them. The
// writer's lock is taken before the registry's is let go, so the records
// land in the order they were decided: a reset that follows a call's end
// lands after it.
func (d *Den) unlockCalls() error {
	r := &d.calls
	writes := r.writes
	r.writes = nil
	if len(writes) == 0 {
		r.mu.Unlock()
		return nil
	}
	r.writing.Lock()
	r.mu.Unlock()
	defer r.writing.Unlock()
	var reset error
	for _, w := range writes {
		var err error
		if w.ms == 0 {
			_, err = d.db.Exec(`UPDATE den_channels SET call_record = NULL WHERE id = ?`, w.channel)
			reset = errors.Join(reset, err)
		} else {
			_, err = d.db.Exec(`UPDATE den_channels SET call_record = ? WHERE id = ? AND kind = 'voice'`, w.ms, w.channel)
		}
		if err != nil {
			d.log.Errorf("write channel %d's call record: %v", w.channel, err)
		}
	}
	return reset
}

// nextRecord is when the next call reaches a minute past what the den kept
// of its channel's record, as Unix milliseconds, or 0 for none.
func (d *Den) nextRecord() int64 {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	var next int64
	for _, room := range r.rooms {
		if mark := room.nextMark(); !mark.IsZero() && (next == 0 || mark.UnixMilli() < next) {
			next = mark.UnixMilli()
		}
	}
	return next
}

// recordDue writes the length of each call that reached a minute past what
// the den kept of its channel's record.
func (d *Den) recordDue() {
	r := &d.calls
	r.mu.Lock()
	now := d.now()
	for cid, room := range r.rooms {
		if mark := room.nextMark(); !mark.IsZero() && !now.Before(mark) {
			d.recordLocked(cid, room, now)
		}
	}
	d.unlockCalls()
}

// staffVoiceChannel returns a voice channel staff see, for its record.
func (d *Den) staffVoiceChannel(ctx context.Context, s *Session, id string) (int64, error) {
	if !IsStaff(s.Role) {
		return 0, errStaffOnly
	}
	cid, err := denproto.ParseID(id)
	if err != nil {
		return 0, errChannelNotFound
	}
	c, err := d.channel(ctx, d.db, cid)
	if err != nil {
		return 0, err
	}
	if c.Kind != denproto.KindVoice || !visible(c, s.MemberID, true) {
		return 0, errChannelNotFound
	}
	return cid, nil
}

// CallRecord reads a voice channel's record for staff, as this process
// last decided it.
func (d *Den) CallRecord(ctx context.Context, s *Session, id string) (denproto.CallRecord, error) {
	cid, err := d.staffVoiceChannel(ctx, s, id)
	if err != nil {
		return denproto.CallRecord{}, err
	}
	ms, err := storedRecord(ctx, d.db, cid)
	if err != nil {
		return denproto.CallRecord{}, err
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.records[cid]; ok {
		ms = v
	}
	return denproto.CallRecord{RecordMS: ms}, nil
}

// ResetCallRecord clears a voice channel's record at staff's word. A call
// going on in it shows from then on with nothing to beat, and writes none.
func (d *Den) ResetCallRecord(ctx context.Context, s *Session, id string) error {
	cid, err := d.staffVoiceChannel(ctx, s, id)
	if err != nil {
		return err
	}
	r := &d.calls
	r.mu.Lock()
	r.records[cid] = 0
	r.writes = append(r.writes, recordWrite{channel: cid})
	if room := r.rooms[cid]; room != nil {
		room.record, room.counts = 0, false
		d.publishCallsLocked(cid)
	}
	if err := d.unlockCalls(); err != nil {
		return err
	}
	d.log.Infof("Member %d reset the call record of channel %d", s.MemberID, cid)
	return nil
}
