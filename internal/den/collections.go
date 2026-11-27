package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Collections: channel-scoped shared collections managed by staff, and
// den-wide personal collections owned by a single member. Collection names are
// sealed with the den's data key, bound to each row's ID.

var (
	errCollectionNotFound     = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such collection")
	errCollectionItemNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "message is not in this collection")
)

func collectionNameAD(id int64) []byte {
	return []byte("den_collections.name:" + strconv.FormatInt(id, 10))
}

// collectionItemVisibleSQL filters joined den_messages (m) and den_channels (c)
// to non-expired messages the member can currently see.
func collectionItemVisibleSQL(staffExpr, memberExpr, allowNotesExpr, nowExpr string) string {
	return `(((c.kind = 'text' AND ` + channelVisibleSQL(staffExpr, memberExpr) + `) OR ` +
		`(c.kind = 'dm' AND (c.dm_low = ` + memberExpr + ` OR c.dm_high = ` + memberExpr + `)) OR ` +
		`(` + allowNotesExpr + ` AND c.kind = 'notes' AND c.notes_of = ` + memberExpr + `)) AND ` +
		`(m.expires_at IS NULL OR m.expires_at > ` + nowExpr + `))`
}

type storedCollection struct {
	id        int64
	channelID sql.NullInt64
	ownerID   sql.NullInt64
	name      string
	whoAdds   string
	createdBy int64
	createdAt int64
}

func (c storedCollection) isShared() bool { return c.channelID.Valid }

func (c storedCollection) wire(count int) denproto.Collection {
	out := denproto.Collection{
		ID:        denproto.FormatID(c.id),
		Name:      c.name,
		Count:     count,
		CreatedAt: c.createdAt,
	}
	if c.channelID.Valid {
		out.ChannelID = denproto.FormatID(c.channelID.Int64)
		out.WhoAdds = c.whoAdds
		out.CreatedBy = denproto.FormatID(c.createdBy)
	}
	return out
}

func (d *Den) loadCollection(ctx context.Context, q querier, id int64) (storedCollection, error) {
	var c storedCollection
	var sealed []byte
	err := q.QueryRowContext(ctx, `SELECT id, channel_id, owner_id, name, who_adds, created_by, created_at FROM den_collections WHERE id = ?`, id).
		Scan(&c.id, &c.channelID, &c.ownerID, &sealed, &c.whoAdds, &c.createdBy, &c.createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storedCollection{}, errCollectionNotFound
	}
	if err != nil {
		return storedCollection{}, err
	}
	plain, err := d.v.Open(sealed, collectionNameAD(c.id))
	if err != nil {
		return storedCollection{}, err
	}
	c.name = string(plain)
	return c, nil
}

func (d *Den) visibleCollectionCount(ctx context.Context, q querier, colID int64, staff bool, member int64, allowNotes bool, now int64) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM den_collection_items ci
		JOIN den_messages m ON m.id = ci.message_id
		JOIN den_channels c ON c.id = m.channel_id
		WHERE ci.collection_id = ?5 AND `+collectionItemVisibleSQL("?1", "?2", "?3", "?4"),
		staff, member, allowNotes, now, colID).Scan(&count)
	return count, err
}

// collectionsFor lists a member's personal collections and every shared
// collection in channels they currently see, each with its visible item count.
func (d *Den) collectionsFor(ctx context.Context, q querier, member int64, staff bool, version int) ([]denproto.Collection, error) {
	allowNotes := version >= denproto.NotesVersion
	now := d.now().UnixMilli()
	rows, err := q.QueryContext(ctx, `SELECT col.id, col.channel_id, col.owner_id, col.name, col.who_adds, col.created_by, col.created_at,
		(SELECT count(*) FROM den_collection_items ci
		 JOIN den_messages m ON m.id = ci.message_id
		 JOIN den_channels c ON c.id = m.channel_id
		 WHERE ci.collection_id = col.id AND `+collectionItemVisibleSQL("?1", "?2", "?3", "?4")+`)
		FROM den_collections col
		WHERE col.owner_id = ?2
		   OR (col.channel_id IS NOT NULL AND EXISTS (
		       SELECT 1 FROM den_channels c WHERE c.id = col.channel_id AND c.kind = 'text' AND `+channelVisibleSQL("?1", "?2")+`
		   ))
		ORDER BY col.created_at ASC, col.id ASC`,
		staff, member, allowNotes, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []denproto.Collection{}
	for rows.Next() {
		var c storedCollection
		var sealed []byte
		var count int
		if err := rows.Scan(&c.id, &c.channelID, &c.ownerID, &sealed, &c.whoAdds, &c.createdBy, &c.createdAt, &count); err != nil {
			return nil, err
		}
		plain, err := d.v.Open(sealed, collectionNameAD(c.id))
		if err != nil {
			return nil, err
		}
		c.name = string(plain)
		out = append(out, c.wire(count))
	}
	return out, rows.Err()
}

// checkCollectionScope checks uniqueness (case-insensitive) and returns the
// number of existing collections in the scope, excluding skipID (0 for none).
func (d *Den) checkCollectionScope(ctx context.Context, tx *sql.Tx, where string, scopeID int64, name string, skipID int64) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM den_collections WHERE `+where, scopeID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	folded := denproto.FoldName(name)
	count := 0
	for rows.Next() {
		var id int64
		var sealed []byte
		if err := rows.Scan(&id, &sealed); err != nil {
			return 0, err
		}
		count++
		if id == skipID {
			continue
		}
		plain, err := d.v.Open(sealed, collectionNameAD(id))
		if err != nil {
			return 0, err
		}
		if denproto.FoldName(string(plain)) == folded {
			return 0, denproto.Errorf(http.StatusConflict, denproto.CodeCollectionNameTaken, "another collection here already has that name")
		}
	}
	return count, rows.Err()
}

// ListCollections returns all personal collections of the caller and all shared
// collections in channels they can see, with fresh visible item counts.
func (d *Den) ListCollections(ctx context.Context, s *Session) (denproto.CollectionList, error) {
	list, err := d.collectionsFor(ctx, d.db, s.MemberID, IsStaff(s.Role), s.Version)
	if err != nil {
		return denproto.CollectionList{}, err
	}
	return denproto.CollectionList{Collections: list}, nil
}

// CreatePersonalCollection creates a personal collection for the caller.
func (d *Den) CreatePersonalCollection(ctx context.Context, s *Session, req denproto.CollectionRequest) (denproto.Collection, error) {
	if req.WhoAdds != nil {
		return denproto.Collection{}, invalid("who_adds: personal collections do not have who_adds")
	}
	if req.Name == nil {
		return denproto.Collection{}, invalid("name: required")
	}
	name, err := denproto.CleanCollectionName(*req.Name)
	if err != nil {
		return denproto.Collection{}, invalid("name: %v", err)
	}
	d.collections.Lock()
	defer d.collections.Unlock()

	now := d.now().UnixMilli()
	var col storedCollection
	err = d.tx(ctx, func(tx *sql.Tx) error {
		n, err := d.checkCollectionScope(ctx, tx, `owner_id = ?`, s.MemberID, name, 0)
		if err != nil {
			return err
		}
		if n >= denproto.MaxPersonalCollections {
			return invalid("you can have at most %d personal collections on a den", denproto.MaxPersonalCollections)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_collections (owner_id, name, created_by, created_at) VALUES (?, ?, ?, ?)`,
			s.MemberID, []byte{}, s.MemberID, now)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		sealed, err := d.v.Seal([]byte(name), collectionNameAD(id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_collections SET name = ? WHERE id = ?`, sealed, id); err != nil {
			return err
		}
		col = storedCollection{
			id:        id,
			ownerID:   sql.NullInt64{Int64: s.MemberID, Valid: true},
			name:      name,
			createdBy: s.MemberID,
			createdAt: now,
		}
		return nil
	})
	if err != nil {
		return denproto.Collection{}, err
	}
	wire := col.wire(0)
	if err := d.Hub.Publish(denproto.EventCollectionCreated, wire, OnlyMember(s.MemberID)); err != nil {
		return wire, err
	}
	d.log.Infof("Member %d created personal collection %d", s.MemberID, col.id)
	return wire, nil
}

// CreateChannelCollection creates a shared collection in a text channel. Staff only.
func (d *Den) CreateChannelCollection(ctx context.Context, s *Session, channelID string, req denproto.CollectionRequest) (denproto.Collection, error) {
	if !IsStaff(s.Role) {
		return denproto.Collection{}, forbidden("only moderators and the owner manage channel collections")
	}
	if req.Name == nil {
		return denproto.Collection{}, invalid("name: required")
	}
	name, err := denproto.CleanCollectionName(*req.Name)
	if err != nil {
		return denproto.Collection{}, invalid("name: %v", err)
	}
	whoAdds := denproto.CollectionAddStaff
	if req.WhoAdds != nil {
		if err := denproto.CheckWhoAdds(*req.WhoAdds); err != nil {
			return denproto.Collection{}, invalid("who_adds: %v", err)
		}
		whoAdds = *req.WhoAdds
	}
	d.collections.Lock()
	defer d.collections.Unlock()

	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return denproto.Collection{}, err
	}
	if c.Kind != denproto.KindText {
		return denproto.Collection{}, invalid("only text channels hold shared collections")
	}

	now := d.now().UnixMilli()
	var col storedCollection
	err = d.tx(ctx, func(tx *sql.Tx) error {
		n, err := d.checkCollectionScope(ctx, tx, `channel_id = ?`, cid, name, 0)
		if err != nil {
			return err
		}
		if n >= denproto.MaxChannelCollections {
			return invalid("a channel can have at most %d collections", denproto.MaxChannelCollections)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_collections (channel_id, name, who_adds, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			cid, []byte{}, whoAdds, s.MemberID, now)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		sealed, err := d.v.Seal([]byte(name), collectionNameAD(id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_collections SET name = ? WHERE id = ?`, sealed, id); err != nil {
			return err
		}
		col = storedCollection{
			id:        id,
			channelID: sql.NullInt64{Int64: cid, Valid: true},
			name:      name,
			whoAdds:   whoAdds,
			createdBy: s.MemberID,
			createdAt: now,
		}
		return d.audit(ctx, tx, auditLine{
			kind: denproto.AuditCollectionCreated,
			by:   s.MemberID,
			details: denproto.AuditDetails{
				ChannelID: c.ID,
				Channel:   c.Name,
				Ref:       denproto.FormatID(id),
				Name:      name,
			},
		})
	})
	if err != nil {
		return denproto.Collection{}, err
	}
	d.audited()
	wire := col.wire(0)
	if err := d.Hub.Publish(denproto.EventCollectionCreated, wire, d.audienceOf(c)); err != nil {
		return wire, err
	}
	d.log.Infof("Member %d created collection %d in channel %d", s.MemberID, col.id, cid)
	return wire, nil
}

// UpdateCollection updates a collection's name and (for shared collections) who_adds.
func (d *Den) UpdateCollection(ctx context.Context, s *Session, collectionID string, req denproto.CollectionRequest) (denproto.Collection, error) {
	colID, err := denproto.ParseID(collectionID)
	if err != nil {
		return denproto.Collection{}, errCollectionNotFound
	}
	var newName *string
	if req.Name != nil {
		clean, err := denproto.CleanCollectionName(*req.Name)
		if err != nil {
			return denproto.Collection{}, invalid("name: %v", err)
		}
		newName = &clean
	}
	d.collections.Lock()
	defer d.collections.Unlock()

	col, err := d.loadCollection(ctx, d.db, colID)
	if err != nil {
		return denproto.Collection{}, err
	}
	var ch denproto.Channel
	if col.isShared() {
		ch, err = d.channel(ctx, d.db, col.channelID.Int64)
		if err != nil {
			return denproto.Collection{}, errCollectionNotFound
		}
		ok, err := d.sees(ctx, d.db, ch, s)
		if err != nil {
			return denproto.Collection{}, err
		}
		if !ok {
			return denproto.Collection{}, errCollectionNotFound
		}
		if !IsStaff(s.Role) {
			return denproto.Collection{}, forbidden("only moderators and the owner manage channel collections")
		}
		if req.WhoAdds != nil {
			if err := denproto.CheckWhoAdds(*req.WhoAdds); err != nil {
				return denproto.Collection{}, invalid("who_adds: %v", err)
			}
		}
	} else {
		if !col.ownerID.Valid || col.ownerID.Int64 != s.MemberID {
			return denproto.Collection{}, errCollectionNotFound
		}
		if req.WhoAdds != nil {
			return denproto.Collection{}, invalid("who_adds: personal collections do not have who_adds")
		}
	}

	nextName := col.name
	if newName != nil {
		nextName = *newName
	}
	nextWho := col.whoAdds
	if req.WhoAdds != nil {
		nextWho = *req.WhoAdds
	}
	nameChanged := nextName != col.name
	whoChanged := col.isShared() && nextWho != col.whoAdds
	allowNotes := s.Version >= denproto.NotesVersion
	if !nameChanged && !whoChanged {
		count, err := d.visibleCollectionCount(ctx, d.db, col.id, IsStaff(s.Role), s.MemberID, allowNotes, d.now().UnixMilli())
		if err != nil {
			return denproto.Collection{}, err
		}
		return col.wire(count), nil
	}

	var count int
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if nameChanged {
			where, scopeID := `owner_id = ?`, col.ownerID.Int64
			if col.isShared() {
				where, scopeID = `channel_id = ?`, col.channelID.Int64
			}
			if _, err := d.checkCollectionScope(ctx, tx, where, scopeID, nextName, col.id); err != nil {
				return err
			}
			sealed, err := d.v.Seal([]byte(nextName), collectionNameAD(col.id))
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_collections SET name = ? WHERE id = ?`, sealed, col.id); err != nil {
				return err
			}
		}
		if whoChanged {
			if _, err := tx.ExecContext(ctx, `UPDATE den_collections SET who_adds = ? WHERE id = ?`, nextWho, col.id); err != nil {
				return err
			}
		}
		var err error
		if count, err = d.visibleCollectionCount(ctx, tx, col.id, IsStaff(s.Role), s.MemberID, allowNotes, d.now().UnixMilli()); err != nil {
			return err
		}
		if !col.isShared() {
			return nil
		}
		var changes []denproto.AuditChange
		if nameChanged {
			changes = append(changes, denproto.AuditChange{Field: "name", From: col.name, To: nextName})
		}
		if whoChanged {
			changes = append(changes, denproto.AuditChange{Field: "who_adds", From: col.whoAdds, To: nextWho})
		}
		return d.audit(ctx, tx, auditLine{
			kind: denproto.AuditCollectionUpdated,
			by:   s.MemberID,
			details: denproto.AuditDetails{
				ChannelID: ch.ID,
				Channel:   ch.Name,
				Ref:       denproto.FormatID(col.id),
				Name:      nextName,
				Changes:   changes,
			},
		})
	})
	if err != nil {
		return denproto.Collection{}, err
	}
	col.name, col.whoAdds = nextName, nextWho
	wire := col.wire(count)
	aud := OnlyMember(s.MemberID)
	if col.isShared() {
		d.audited()
		aud = d.audienceOf(ch)
	}
	if err := d.Hub.Publish(denproto.EventCollectionUpdated, wire, aud); err != nil {
		return wire, err
	}
	d.log.Infof("Member %d updated collection %d", s.MemberID, col.id)
	return wire, nil
}

// DeleteCollection deletes a shared collection (staff only, audited) or a
// personal collection (owner only).
func (d *Den) DeleteCollection(ctx context.Context, s *Session, collectionID string) error {
	colID, err := denproto.ParseID(collectionID)
	if err != nil {
		return errCollectionNotFound
	}
	d.collections.Lock()
	defer d.collections.Unlock()

	col, err := d.loadCollection(ctx, d.db, colID)
	if err != nil {
		return err
	}
	var ch denproto.Channel
	if col.isShared() {
		ch, err = d.channel(ctx, d.db, col.channelID.Int64)
		if err != nil {
			return errCollectionNotFound
		}
		ok, err := d.sees(ctx, d.db, ch, s)
		if err != nil {
			return err
		}
		if !ok {
			return errCollectionNotFound
		}
		if !IsStaff(s.Role) {
			return forbidden("only moderators and the owner manage channel collections")
		}
	} else if !col.ownerID.Valid || col.ownerID.Int64 != s.MemberID {
		return errCollectionNotFound
	}

	err = d.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM den_collections WHERE id = ?`, col.id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errCollectionNotFound
		}
		if !col.isShared() {
			return nil
		}
		return d.audit(ctx, tx, auditLine{
			kind: denproto.AuditCollectionDeleted,
			by:   s.MemberID,
			details: denproto.AuditDetails{
				ChannelID: ch.ID,
				Channel:   ch.Name,
				Ref:       denproto.FormatID(col.id),
				Name:      col.name,
			},
		})
	})
	if err != nil {
		return err
	}
	ev := denproto.CollectionDeleted{ID: denproto.FormatID(col.id)}
	aud := OnlyMember(s.MemberID)
	if col.isShared() {
		d.audited()
		ev.ChannelID = ch.ID
		aud = d.audienceOf(ch)
	}
	d.log.Infof("Member %d deleted collection %d", s.MemberID, col.id)
	return d.Hub.Publish(denproto.EventCollectionDeleted, ev, aud)
}

// collectionItemsBudget matches historyBudget so a collection items response
// stays within the client's 1 MiB body limit with room for the envelope.
var collectionItemsBudget = historyBudget

const collectionPageChunk = 50

// CollectionItemsQuery selects a page of a collection's items, newest added
// first, optionally continuing strictly before (BeforeAt, BeforeID).
type CollectionItemsQuery struct {
	BeforeAt int64
	BeforeID int64
}

// CollectionItems returns a page of a collection and its currently visible,
// non-expired items in newest-added-first order.
func (d *Den) CollectionItems(ctx context.Context, s *Session, collectionID string, q CollectionItemsQuery) (denproto.CollectionItems, error) {
	if q.BeforeAt < 0 || q.BeforeID < 0 || (q.BeforeID == 0 && q.BeforeAt != 0) {
		return denproto.CollectionItems{}, invalid("before_at and before_id must be set together")
	}
	colID, err := denproto.ParseID(collectionID)
	if err != nil {
		return denproto.CollectionItems{}, errCollectionNotFound
	}
	col, err := d.loadCollection(ctx, d.db, colID)
	if err != nil {
		return denproto.CollectionItems{}, err
	}
	if col.isShared() {
		ch, err := d.channel(ctx, d.db, col.channelID.Int64)
		if err != nil {
			return denproto.CollectionItems{}, errCollectionNotFound
		}
		ok, err := d.sees(ctx, d.db, ch, s)
		if err != nil {
			return denproto.CollectionItems{}, err
		}
		if !ok {
			return denproto.CollectionItems{}, errCollectionNotFound
		}
	} else if !col.ownerID.Valid || col.ownerID.Int64 != s.MemberID {
		return denproto.CollectionItems{}, errCollectionNotFound
	}

	type itemRow struct {
		messageID int64
		addedBy   int64
		addedAt   int64
	}
	allowNotes := s.Version >= denproto.NotesVersion
	now := d.now().UnixMilli()
	var itemRows []itemRow
	var totalCount int
	err = d.readTx(ctx, func(tx *sql.Tx) error {
		where := `ci.collection_id = ?5 AND ` + collectionItemVisibleSQL("?1", "?2", "?3", "?4")
		args := []any{IsStaff(s.Role), s.MemberID, allowNotes, now, col.id}
		if q.BeforeID > 0 {
			where += ` AND ci.added_at <= ?6 AND (ci.added_at < ?6 OR ci.message_id < ?7)`
			args = append(args, q.BeforeAt, q.BeforeID)
		}
		rows, err := tx.QueryContext(ctx, `SELECT ci.message_id, ci.added_by, ci.added_at
			FROM den_collection_items ci
			JOIN den_messages m ON m.id = ci.message_id
			JOIN den_channels c ON c.id = m.channel_id
			WHERE `+where+`
			ORDER BY ci.added_at DESC, ci.message_id DESC`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r itemRow
			if err := rows.Scan(&r.messageID, &r.addedBy, &r.addedAt); err != nil {
				rows.Close()
				return err
			}
			itemRows = append(itemRows, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if q.BeforeID == 0 {
			totalCount = len(itemRows)
			return nil
		}
		totalCount, err = d.visibleCollectionCount(ctx, tx, col.id, IsStaff(s.Role), s.MemberID, allowNotes, now)
		return err
	})
	if err != nil {
		return denproto.CollectionItems{}, err
	}
	items := []denproto.CollectionItem{}
	used := 0
	hasMore := false
	for start := 0; start < len(itemRows) && !hasMore; start += collectionPageChunk {
		chunk := itemRows[start:min(start+collectionPageChunk, len(itemRows))]
		mids := make([]int64, len(chunk))
		for i, r := range chunk {
			mids[i] = r.messageID
		}
		msgs, err := d.queryMessages(ctx, s.MemberID, `SELECT `+messageColumns+` FROM den_messages WHERE id IN (?`+
			strings.Repeat(", ?", len(mids)-1)+`)`, anys(mids)...)
		if err != nil {
			return denproto.CollectionItems{}, err
		}
		byID := make(map[string]denproto.Message, len(msgs))
		for _, m := range msgs {
			byID[m.ID] = m
		}
		for _, r := range chunk {
			msg, ok := byID[denproto.FormatID(r.messageID)]
			if !ok {
				continue
			}
			item := denproto.CollectionItem{
				Message: msg,
				AddedBy: denproto.FormatID(r.addedBy),
				AddedAt: r.addedAt,
			}
			b, err := json.Marshal(item)
			if err != nil {
				return denproto.CollectionItems{}, err
			}
			if used+len(b)+1 > collectionItemsBudget && len(items) > 0 {
				hasMore = true
				break
			}
			used += len(b) + 1
			items = append(items, item)
		}
	}
	if q.BeforeID == 0 && !hasMore {
		totalCount = len(items)
	}
	resp := denproto.CollectionItems{
		Collection: col.wire(totalCount),
		Items:      items,
		HasMore:    hasMore,
	}
	if hasMore {
		last := items[len(items)-1]
		resp.NextBeforeAt = last.AddedAt
		resp.NextBeforeID = last.Message.ID
	}
	return resp, nil
}

// AddCollectionItem adds a message to a collection. Idempotent if already present.
func (d *Den) AddCollectionItem(ctx context.Context, s *Session, collectionID string, req denproto.CollectionItemRequest) (denproto.CollectionItemEvent, error) {
	colID, err := denproto.ParseID(collectionID)
	if err != nil {
		return denproto.CollectionItemEvent{}, errCollectionNotFound
	}
	mid, err := denproto.ParseID(req.MessageID)
	if err != nil {
		return denproto.CollectionItemEvent{}, errMessageNotFound
	}
	d.collections.Lock()
	defer d.collections.Unlock()

	col, err := d.loadCollection(ctx, d.db, colID)
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	var colCh denproto.Channel
	if col.isShared() {
		colCh, err = d.channel(ctx, d.db, col.channelID.Int64)
		if err != nil {
			return denproto.CollectionItemEvent{}, errCollectionNotFound
		}
		ok, err := d.sees(ctx, d.db, colCh, s)
		if err != nil {
			return denproto.CollectionItemEvent{}, err
		}
		if !ok {
			return denproto.CollectionItemEvent{}, errCollectionNotFound
		}
		if col.whoAdds != denproto.CollectionAddEveryone && !IsStaff(s.Role) {
			return denproto.CollectionItemEvent{}, forbidden("only moderators and the owner can add messages to this collection")
		}
	} else if !col.ownerID.Valid || col.ownerID.Int64 != s.MemberID {
		return denproto.CollectionItemEvent{}, errCollectionNotFound
	}

	now := d.now().UnixMilli()
	var msgChannelID int64
	var expiresAt sql.NullInt64
	err = d.db.QueryRowContext(ctx, `SELECT channel_id, expires_at FROM den_messages WHERE id = ?`, mid).Scan(&msgChannelID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || (expiresAt.Valid && expiresAt.Int64 <= now) {
		return denproto.CollectionItemEvent{}, errMessageNotFound
	}
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	msgCh, err := d.channel(ctx, d.db, msgChannelID)
	if err != nil {
		return denproto.CollectionItemEvent{}, errMessageNotFound
	}
	seesMsg, err := d.sees(ctx, d.db, msgCh, s)
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if !seesMsg {
		return denproto.CollectionItemEvent{}, errMessageNotFound
	}
	if col.isShared() && msgChannelID != col.channelID.Int64 {
		return denproto.CollectionItemEvent{}, invalid("shared collections only hold messages from their channel")
	}

	allowNotes := s.Version >= denproto.NotesVersion
	var addedBy int64
	var addedAt int64
	var count int
	var inserted bool
	err = d.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT added_by, added_at FROM den_collection_items WHERE collection_id = ? AND message_id = ?`, col.id, mid).
			Scan(&addedBy, &addedAt)
		if err == nil {
			count, err = d.visibleCollectionCount(ctx, tx, col.id, IsStaff(s.Role), s.MemberID, allowNotes, now)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var total int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_collection_items ci
			JOIN den_messages m ON m.id = ci.message_id
			WHERE ci.collection_id = ? AND (m.expires_at IS NULL OR m.expires_at > ?)`, col.id, now).Scan(&total); err != nil {
			return err
		}
		if total >= denproto.MaxCollectionItems {
			return invalid("a collection holds at most %d messages", denproto.MaxCollectionItems)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (?, ?, ?, ?)`,
			col.id, mid, s.MemberID, now); err != nil {
			return err
		}
		addedBy, addedAt, inserted = s.MemberID, now, true
		count, err = d.visibleCollectionCount(ctx, tx, col.id, IsStaff(s.Role), s.MemberID, allowNotes, now)
		return err
	})
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	ev := denproto.CollectionItemEvent{
		CollectionID: denproto.FormatID(col.id),
		MessageID:    denproto.FormatID(mid),
		AddedBy:      denproto.FormatID(addedBy),
		AddedAt:      addedAt,
		Count:        count,
	}
	aud := OnlyMember(s.MemberID)
	if col.isShared() {
		ev.ChannelID = colCh.ID
		aud = d.audienceOf(colCh)
	}
	if inserted {
		if err := d.Hub.Publish(denproto.EventCollectionItemAdded, ev, aud); err != nil {
			return ev, err
		}
		d.log.Infof("Member %d added message %d to collection %d", s.MemberID, mid, col.id)
	}
	return ev, nil
}

// RemoveCollectionItem removes a message from a collection. On shared
// collections, allowed for the member who added it or staff; staff removing
// another member's item writes an audit line.
func (d *Den) RemoveCollectionItem(ctx context.Context, s *Session, collectionID, messageID string) (denproto.CollectionItemEvent, error) {
	colID, err := denproto.ParseID(collectionID)
	if err != nil {
		return denproto.CollectionItemEvent{}, errCollectionNotFound
	}
	mid, err := denproto.ParseID(messageID)
	if err != nil {
		return denproto.CollectionItemEvent{}, errCollectionItemNotFound
	}
	d.collections.Lock()
	defer d.collections.Unlock()

	col, err := d.loadCollection(ctx, d.db, colID)
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	var colCh denproto.Channel
	if col.isShared() {
		colCh, err = d.channel(ctx, d.db, col.channelID.Int64)
		if err != nil {
			return denproto.CollectionItemEvent{}, errCollectionNotFound
		}
		ok, err := d.sees(ctx, d.db, colCh, s)
		if err != nil {
			return denproto.CollectionItemEvent{}, err
		}
		if !ok {
			return denproto.CollectionItemEvent{}, errCollectionNotFound
		}
	} else if !col.ownerID.Valid || col.ownerID.Int64 != s.MemberID {
		return denproto.CollectionItemEvent{}, errCollectionNotFound
	}

	allowNotes := s.Version >= denproto.NotesVersion
	now := d.now().UnixMilli()
	var count int
	var audited bool
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var addedBy int64
		err := tx.QueryRowContext(ctx, `SELECT added_by FROM den_collection_items WHERE collection_id = ? AND message_id = ?`, col.id, mid).Scan(&addedBy)
		if errors.Is(err, sql.ErrNoRows) {
			return errCollectionItemNotFound
		}
		if err != nil {
			return err
		}
		if col.isShared() && addedBy != s.MemberID && !IsStaff(s.Role) {
			return forbidden("only who added this message or staff can remove it")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_collection_items WHERE collection_id = ? AND message_id = ?`, col.id, mid); err != nil {
			return err
		}
		if count, err = d.visibleCollectionCount(ctx, tx, col.id, IsStaff(s.Role), s.MemberID, allowNotes, now); err != nil {
			return err
		}
		if col.isShared() && addedBy != s.MemberID {
			audited = true
			return d.audit(ctx, tx, auditLine{
				kind:   denproto.AuditCollectionItemRemoved,
				by:     s.MemberID,
				member: addedBy,
				details: denproto.AuditDetails{
					ChannelID: colCh.ID,
					Channel:   colCh.Name,
					Ref:       denproto.FormatID(col.id),
					Name:      col.name,
				},
			})
		}
		return nil
	})
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if audited {
		d.audited()
	}
	ev := denproto.CollectionItemEvent{
		CollectionID: denproto.FormatID(col.id),
		MessageID:    denproto.FormatID(mid),
		Count:        count,
	}
	aud := OnlyMember(s.MemberID)
	if col.isShared() {
		ev.ChannelID = colCh.ID
		aud = d.audienceOf(colCh)
	}
	if err := d.Hub.Publish(denproto.EventCollectionItemRemoved, ev, aud); err != nil {
		return ev, err
	}
	d.log.Infof("Member %d removed message %d from collection %d", s.MemberID, mid, col.id)
	return ev, nil
}

// MessageCollections returns the IDs of collections accessible to the caller
// (their personal collections and the message's channel's shared collections)
// that currently hold messageID.
func (d *Den) MessageCollections(ctx context.Context, s *Session, messageID string) (denproto.MessageCollections, error) {
	mid, err := denproto.ParseID(messageID)
	if err != nil {
		return denproto.MessageCollections{}, errMessageNotFound
	}
	now := d.now().UnixMilli()
	var msgChannelID int64
	var expiresAt sql.NullInt64
	err = d.db.QueryRowContext(ctx, `SELECT channel_id, expires_at FROM den_messages WHERE id = ?`, mid).Scan(&msgChannelID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || (expiresAt.Valid && expiresAt.Int64 <= now) {
		return denproto.MessageCollections{}, errMessageNotFound
	}
	if err != nil {
		return denproto.MessageCollections{}, err
	}
	msgCh, err := d.channel(ctx, d.db, msgChannelID)
	if err != nil {
		return denproto.MessageCollections{}, errMessageNotFound
	}
	ok, err := d.sees(ctx, d.db, msgCh, s)
	if err != nil {
		return denproto.MessageCollections{}, err
	}
	if !ok {
		return denproto.MessageCollections{}, errMessageNotFound
	}

	rows, err := d.db.QueryContext(ctx, `SELECT col.id FROM den_collection_items ci
		JOIN den_collections col ON col.id = ci.collection_id
		WHERE ci.message_id = ? AND (col.owner_id = ? OR col.channel_id = ?)
		ORDER BY col.id ASC`, mid, s.MemberID, msgChannelID)
	if err != nil {
		return denproto.MessageCollections{}, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return denproto.MessageCollections{}, err
		}
		ids = append(ids, denproto.FormatID(id))
	}
	return denproto.MessageCollections{CollectionIDs: ids}, rows.Err()
}
