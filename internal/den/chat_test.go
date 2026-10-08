package den

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// chat sets up a den with an owner (alice) and a member (bob).
func chatFixture(t *testing.T) (f *fixture, owner, member *Session) {
	t.Helper()
	f = newFixture(t)
	joined, err := f.join(f.create(), "alice", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	owner, _ = f.d.Authenticate(context.Background(), joined.Token)
	inv, err := f.d.CreateInvite(context.Background(), owner, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	joined, err = f.join(inv.Code, "bob", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	member, _ = f.d.Authenticate(context.Background(), joined.Token)
	return f, owner, member
}

func ptr[T any](v T) *T { return &v }

func (f *fixture) newChannel(s *Session, name string, req denproto.ChannelRequest) denproto.Channel {
	f.t.Helper()
	req.Name = &name
	c, err := f.d.CreateChannel(context.Background(), s, req)
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fixture) order(staff bool) string {
	f.t.Helper()
	cs, err := f.d.channels(context.Background(), f.db, staff)
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, c := range cs {
		g := "-"
		if c.GroupID != nil {
			g = *c.GroupID
		}
		names = append(names, fmt.Sprintf("%s:%s%d", c.Name, g, c.Position))
	}
	return strings.Join(names, " ")
}

func TestChannelOrdering(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.newChannel(owner, "random", denproto.ChannelRequest{})
	f.newChannel(owner, "rules", denproto.ChannelRequest{Position: ptr(0)})
	if got := f.order(false); got != "rules:-0 general:-1 random:-2" {
		t.Fatalf("after inserting at 0: %s", got)
	}
	if _, err := f.d.CreateChannel(ctx, member, denproto.ChannelRequest{Name: ptr("x")}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("a member created a channel: %v", err)
	}

	g, err := f.d.CreateGroup(ctx, owner, denproto.GroupRequest{Name: ptr("Games")})
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := f.d.channels(ctx, f.db, true)
	random := cs[2]
	if _, err := f.d.UpdateChannel(ctx, owner, random.ID, denproto.ChannelRequest{GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	if got := f.order(false); got != "rules:-0 general:-1 random:"+g.ID+"0" {
		t.Fatalf("after moving into a group: %s", got)
	}
	if _, err := f.d.UpdateChannel(ctx, owner, cs[0].ID, denproto.ChannelRequest{Position: ptr(5)}); err != nil {
		t.Fatal(err)
	}
	if got := f.order(false); got != "general:-0 rules:-1 random:"+g.ID+"0" {
		t.Fatalf("after moving to the end: %s", got)
	}
	if err := f.d.DeleteGroup(ctx, owner, g.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.order(false); got != "general:-0 rules:-1 random:-2" {
		t.Fatalf("after deleting the group: %s", got)
	}
}

func TestStaffOnlyChannels(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	f.newChannel(owner, "general", denproto.ChannelRequest{})
	staff := f.newChannel(owner, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	if got := f.order(false); got != "general:-0" {
		t.Fatalf("a member sees %s", got)
	}
	if _, err := f.d.History(ctx, member, staff.ID, HistoryQuery{}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("a member read a staff-only channel: %v", err)
	}
	snap, err := f.d.snapshot(ctx, member.MemberID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Channels) != 1 || len(snap.ReadStates) != 1 || len(snap.Members) != 2 || snap.Me.Username != "bob" {
		t.Fatalf("member snapshot %+v", snap)
	}
	// Reorders reach members without the hidden channel's ID.
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	f.newChannel(owner, "later", denproto.ChannelRequest{})
	for e := range sub.Events {
		if strings.Contains(string(e.D), staff.ID) && strings.Contains(string(e.D), `"`+staff.ID+`"`) {
			t.Fatalf("a member got %s: %s", e.T, e.D)
		}
		if e.T == denproto.EventChannelsReordered {
			break
		}
	}
}

// seed inserts n messages directly, for paging tests.
func (f *fixture) seed(channel denproto.Channel, author *Session, n int) {
	f.t.Helper()
	cid, _ := denproto.ParseID(channel.ID)
	err := f.d.tx(context.Background(), func(tx *sql.Tx) error {
		for i := range n {
			res, err := tx.Exec(`INSERT INTO den_messages (channel_id, author_id, created_at, text, nonce) VALUES (?, ?, ?, x'', ?)`,
				cid, author.MemberID, f.clock.UnixMilli(), denproto.Random(16))
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			sealed, err := f.v.Seal([]byte(fmt.Sprintf("message %d", i)), textAD(id))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE den_messages SET text = ? WHERE id = ?`, sealed, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func texts(h denproto.History) string {
	var out []string
	for _, m := range h.Messages {
		out = append(out, strings.TrimPrefix(m.Text, "message "))
	}
	return strings.Join(out, ",")
}

func TestHistoryPaging(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.seed(c, owner, 6000)

	newest, err := f.d.History(ctx, member, c.ID, HistoryQuery{Limit: 3})
	if err != nil || texts(newest) != "5997,5998,5999" || !newest.HasOlder || newest.HasNewer {
		t.Fatalf("newest: %s %+v %v", texts(newest), newest.HasOlder, err)
	}
	first, _ := denproto.ParseID(newest.Messages[0].ID)
	older, _ := f.d.History(ctx, member, c.ID, HistoryQuery{Before: first, Limit: 2})
	if texts(older) != "5995,5996" || !older.HasOlder || !older.HasNewer {
		t.Fatalf("before: %s %+v", texts(older), older)
	}
	// Jump 5,000 messages back, then page forward to the present.
	target := first - 4997
	around, _ := f.d.History(ctx, member, c.ID, HistoryQuery{Around: target, Limit: 4})
	if texts(around) != "998,999,1000,1001" || !around.HasOlder || !around.HasNewer {
		t.Fatalf("around: %s %+v", texts(around), around)
	}
	last, _ := denproto.ParseID(around.Messages[3].ID)
	after, _ := f.d.History(ctx, member, c.ID, HistoryQuery{After: last, Limit: 2})
	if texts(after) != "1002,1003" || !after.HasOlder || !after.HasNewer {
		t.Fatalf("after: %s %+v", texts(after), after)
	}
	end, _ := f.d.History(ctx, member, c.ID, HistoryQuery{After: first + 1, Limit: 5})
	if texts(end) != "5999" || end.HasNewer {
		t.Fatalf("after, at the end: %s %+v", texts(end), end)
	}
	if _, err := f.d.History(ctx, member, c.ID, HistoryQuery{Limit: 101}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("limit 101: %v", err)
	}
}

func TestSendEditDelete(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	nonce := denproto.Random(16)
	m, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: nonce, Text: "hello @alice"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: nonce, Text: "hello @alice"})
	if err != nil || again.ID != m.ID {
		t.Fatalf("retry posted %s, want the original %s (%v)", again.ID, m.ID, err)
	}
	reply, err := f.d.Send(ctx, owner, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "hi bob", ReplyTo: m.ID})
	if err != nil || reply.ReplyTo != m.ID || reply.Reply == nil || reply.Reply.Text != "hello @alice" || reply.Reply.AuthorID != m.AuthorID {
		t.Fatalf("reply: %+v %v", reply, err)
	}

	// The sealed text isn't on disk in the clear.
	var raw []byte
	f.db.QueryRow(`SELECT text FROM den_messages WHERE id = ?`, m.ID).Scan(&raw)
	if bytes.Contains(raw, []byte("hello")) {
		t.Fatal("message text is stored in the clear")
	}

	if _, err := f.d.Send(ctx, owner, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", ReplyTo: "999999"}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("a reply to a missing message: %v", err)
	}
	if _, err := f.d.Edit(ctx, owner, m.ID, denproto.EditRequest{Revision: 1, Text: "x"}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("the owner edited bob's message: %v", err)
	}
	edited, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "hello everyone"})
	if err != nil || edited.Revision != 2 || edited.EditedBy != denproto.FormatID(member.MemberID) {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	_, err = f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "from a stale device"})
	if current, ok := Conflict(err); !ok || current.Text != "hello everyone" || current.Revision != 2 {
		t.Fatalf("stale edit: %+v %v", current, err)
	}
	page, _ := f.d.History(ctx, member, c.ID, HistoryQuery{})
	if r := page.Messages[1].Reply; r == nil || r.Text != "hello everyone" {
		t.Fatalf("the reply's preview after an edit: %+v", r)
	}

	if err := f.d.Delete(ctx, member, reply.ID); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob deleted alice's message: %v", err)
	}
	if err := f.d.Delete(ctx, owner, m.ID); err != nil {
		t.Fatalf("the owner deleting a message: %v", err)
	}
	page, _ = f.d.History(ctx, member, c.ID, HistoryQuery{})
	if texts(page) != "hi bob" || page.Messages[0].ReplyTo != m.ID || page.Messages[0].Reply != nil {
		t.Fatalf("after delete: %+v", page)
	}
}

// The den takes tracking out of links in every text it keeps from members,
// then checks the text's limits.
func TestLinksLoseTheirTracking(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	const shared, clean = "https://youtu.be/dQw4w9WgXcQ?si=Xa1B2c3D4e5F6g7H&t=42", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42"
	c := f.newChannel(owner, "general", denproto.ChannelRequest{Description: ptr("rules: " + shared)})
	if c.Description != "rules: "+clean {
		t.Fatalf("description %q", c.Description)
	}
	c, err := f.d.UpdateChannel(ctx, owner, c.ID, denproto.ChannelRequest{Description: ptr("new rules: " + shared)})
	if err != nil || c.Description != "new rules: "+clean {
		t.Fatalf("changed description %q: %v", c.Description, err)
	}
	m, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "look " + shared})
	if err != nil || m.Text != "look "+clean {
		t.Fatalf("sent %q: %v", m.Text, err)
	}
	m, err = f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: m.Revision, Text: "look again " + shared})
	if err != nil || m.Text != "look again "+clean {
		t.Fatalf("edited %q: %v", m.Text, err)
	}
	if page, _ := f.d.History(ctx, owner, c.ID, HistoryQuery{}); texts(page) != "look again "+clean {
		t.Fatalf("history %q", texts(page))
	}
	me, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Bio: ptr("me: " + shared)})
	if err != nil || me.Bio != "me: "+clean {
		t.Fatalf("bio %q: %v", me.Bio, err)
	}

	// The limits apply to the text the den keeps: one that fits only once
	// its tracking is out goes through, and one that grows past them
	// doesn't.
	long := "https://youtu.be/dQw4w9WgXcQ?si=" + strings.Repeat("x", 100)
	fits := strings.Repeat("a", denproto.MaxTextRunes-100) + " " + long
	if _, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: fits}); err != nil {
		t.Fatalf("a text that fits once rewritten: %v", err)
	}
	short := "https://youtu.be/dQw4w9WgXcQ"
	grows := strings.Repeat("a", denproto.MaxTextRunes-len(short)-1) + " " + short
	if _, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: grows}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("a text that grows past the limit: %v", err)
	}
}

func TestMentionsAndReadState(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	send := func(s *Session, text string) denproto.Message {
		m, err := f.d.Send(ctx, s, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	state := func(s *Session, staff bool) denproto.ReadState {
		snap, err := f.d.snapshot(ctx, s.MemberID, staff, 0)
		if err != nil {
			t.Fatal(err)
		}
		return snap.ReadStates[0]
	}
	send(owner, "hey @bob")
	send(owner, "`@bob` in code doesn't count")
	last := send(owner, "@BOB again, and @nobody")
	if st := state(member, false); st.MentionCount != 2 || st.LastMessage != last.ID || st.ReadPosition != "" {
		t.Fatalf("bob's state %+v", st)
	}
	if st := state(owner, true); st.ReadPosition != last.ID || st.MentionCount != 0 {
		t.Fatalf("alice's own messages count as read: %+v", st)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	if err := f.d.MarkRead(ctx, member, c.ID, denproto.ReadRequest{MessageID: last.ID}); err != nil {
		t.Fatal(err)
	}
	e := <-sub.Events
	if e.T != denproto.EventReadStateUpdated || !strings.Contains(string(e.D), `"mention_count":0`) {
		t.Fatalf("read state event %s %s", e.T, e.D)
	}
	// Moving backwards is ignored.
	if err := f.d.MarkRead(ctx, member, c.ID, denproto.ReadRequest{MessageID: "1"}); err != nil {
		t.Fatal(err)
	}
	if st := state(member, false); st.ReadPosition != last.ID || st.MentionCount != 0 {
		t.Fatalf("after reading: %+v", st)
	}
}
