package den

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Managing files (M5): a member lists their own uploads, largest first,
// with what uses each, and the author's edit of a message can change its
// files, which is how they delete one or swap it for another.

// ownFilesBudget bounds the sealed DM messages a page of a member's files
// carries, so the page stays well within a response's limit.
const ownFilesBudget = 600 << 10

// OwnFiles lists a page of the member's files, largest first by the space
// each takes, previews included, with what uses each. after is the cursor
// the page before gave, or empty for the first.
func (d *Den) OwnFiles(ctx context.Context, s *Session, after string) (denproto.OwnFiles, error) {
	page := denproto.OwnFiles{Files: []denproto.OwnFile{}}
	space, last := int64(math.MaxInt64), int64(math.MaxInt64)
	if after != "" {
		a, b, ok := strings.Cut(after, ".")
		var errA, errB error
		space, errA = strconv.ParseInt(a, 10, 64)
		last, errB = strconv.ParseInt(b, 10, 64)
		if !ok || errA != nil || errB != nil {
			return page, invalid("after: not a cursor this den gave")
		}
	}
	rows, err := d.db.QueryContext(ctx, `SELECT f.id, f.message_id, f.name, f.type, f.size, f.width, f.height, f.animated,
		f.thumb_width, f.thumb_height, f.duration_ms, f.sealed, f.created_at, f.size + coalesce(f.thumb_size, 0) AS space,
		m.channel_id, CASE WHEN EXISTS (SELECT 1 FROM den_members WHERE avatar_id = f.id) THEN 'avatar'
			WHEN EXISTS (SELECT 1 FROM den_members WHERE banner_id = f.id) THEN 'banner' ELSE '' END
		FROM den_files f LEFT JOIN den_messages m ON m.id = f.message_id
		WHERE f.uploader_id = ? AND (f.size + coalesce(f.thumb_size, 0), f.id) < (?, ?)
		ORDER BY space DESC, f.id DESC LIMIT ?`, s.MemberID, space, last, denproto.OwnFilesPage+1)
	if err != nil {
		return page, err
	}
	type listed struct {
		file      denproto.OwnFile
		id, space int64
		message   int64
	}
	var files []listed
	for rows.Next() {
		var l listed
		f := &l.file
		var message, channel, w, h, tw, th, duration sql.NullInt64
		var name []byte
		if err := rows.Scan(&l.id, &message, &name, &f.Type, &f.Size, &w, &h, &f.Animated, &tw, &th, &duration, &f.Sealed,
			&f.CreatedAt, &l.space, &channel, &f.Profile); err != nil {
			rows.Close()
			return page, err
		}
		plain, err := d.v.Open(name, fileNameAD(l.id))
		if err != nil {
			rows.Close()
			return page, err
		}
		f.ID, f.Name, f.Width, f.Height, f.Duration = denproto.FormatID(l.id), string(plain), int(w.Int64), int(h.Int64), duration.Int64
		if tw.Valid && th.Valid {
			f.Thumb = &denproto.Thumb{Width: int(tw.Int64), Height: int(th.Int64)}
		}
		if message.Valid {
			l.message = message.Int64
			f.MessageID, f.ChannelID = denproto.FormatID(message.Int64), denproto.FormatID(channel.Int64)
		}
		files = append(files, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return page, err
	}
	more := len(files) > denproto.OwnFilesPage
	files = files[:min(len(files), denproto.OwnFilesPage)]

	// A channel's message gives its file the start of its text, and a DM's
	// comes whole, sealed, for the member's client to open.
	var plainIDs, sealedIDs []int64
	for _, l := range files {
		switch {
		case l.message == 0:
		case l.file.Sealed:
			sealedIDs = append(sealedIDs, l.message)
		default:
			plainIDs = append(plainIDs, l.message)
		}
	}
	excerpts, err := d.excerpts(ctx, plainIDs)
	if err != nil {
		return page, err
	}
	sealed := map[string]denproto.Message{}
	if len(sealedIDs) > 0 {
		ms, err := d.queryMessages(ctx, `SELECT `+messageColumns+` FROM den_messages WHERE id IN (?`+
			strings.Repeat(", ?", len(sealedIDs)-1)+`)`, anys(sealedIDs)...)
		if err != nil {
			return page, err
		}
		for _, m := range ms {
			sealed[m.ID] = m
		}
	}
	included := map[string]bool{}
	budget := ownFilesBudget
	for i, l := range files {
		f := l.file
		if f.MessageID != "" && !f.Sealed {
			f.Excerpt = excerpts[l.message]
		}
		if m, ok := sealed[f.MessageID]; ok && !included[m.ID] {
			if len(m.Sealed) > budget {
				// The rest wait for the next page.
				files, more = files[:i], true
				break
			}
			budget -= len(m.Sealed)
			included[m.ID] = true
			page.Messages = append(page.Messages, m)
		}
		page.Files = append(page.Files, f)
	}
	if more && len(files) > 0 {
		l := files[len(files)-1]
		page.Next = fmt.Sprintf("%d.%d", l.space, l.id)
	}
	return page, nil
}

// excerpts returns the start of each channel message's text, by ID.
func (d *Den) excerpts(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id, text FROM den_messages WHERE key_id IS NULL AND id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`)`, anys(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var sealed []byte
		if err := rows.Scan(&id, &sealed); err != nil {
			return nil, err
		}
		text, err := d.v.Open(sealed, textAD(id))
		if err != nil {
			return nil, err
		}
		out[id] = denproto.Excerpt(string(text))
	}
	return out, rows.Err()
}

func anys(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// setAttachments makes a message's files the ones files names, in that
// order, within tx: files the message has, and its author's uploads
// waiting to be used, of which one made to replace a file takes only the
// place of a file this change drops. The files it drops are deleted. It
// returns their IDs, whether anything changed, and their blobs to remove
// once tx commits.
func setAttachments(ctx context.Context, tx *sql.Tx, message, author int64, files []int64, sealed bool, now time.Time) (
	removed []string, changed bool, blobs [][]byte, err error) {
	keep := map[int64]bool{}
	for _, id := range files {
		keep[id] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM den_files WHERE message_id = ? ORDER BY position`, message)
	if err != nil {
		return nil, false, nil, err
	}
	var had []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, nil, err
		}
		had = append(had, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, nil, err
	}
	on := map[int64]bool{}
	for _, id := range had {
		on[id] = true
	}
	for _, id := range files {
		if on[id] {
			continue
		}
		var replaces sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT replaces FROM den_files
			WHERE id = ? AND uploader_id = ? AND message_id IS NULL AND created_at > ? AND sealed = ?
			AND NOT EXISTS (SELECT 1 FROM den_members WHERE avatar_id = den_files.id OR banner_id = den_files.id)`,
			id, author, now.Add(-uploadExpiry).UnixMilli(), sealed).Scan(&replaces)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil, invalid("attachments: no such upload waiting to be sent")
		}
		if err != nil {
			return nil, false, nil, err
		}
		if replaces.Valid && (!on[replaces.Int64] || keep[replaces.Int64]) {
			return nil, false, nil, invalid("attachments: an upload made to replace a file takes only that file's place")
		}
	}
	for _, id := range had {
		if keep[id] {
			continue
		}
		gone, err := deleteFiles(ctx, tx, `id = ?`, id)
		if err != nil {
			return nil, false, nil, err
		}
		removed, blobs = append(removed, denproto.FormatID(id)), append(blobs, gone...)
	}
	changed = len(had) != len(files)
	for i, id := range files {
		changed = changed || had[i] != id
		if _, err := tx.ExecContext(ctx, `UPDATE den_files SET message_id = ?, position = ?, replaces = NULL WHERE id = ?`,
			message, i, id); err != nil {
			return nil, false, nil, err
		}
	}
	return removed, changed, blobs, nil
}
