package den

import (
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// relaySignal passes a box from one of a member's devices to their others,
// or to the one it names (M20), on their sockets alone, and keeps nothing
// of it: not in the ring, the database or the log. A box of the wrong
// shape, or past the member's limit, goes nowhere; the den can't open one.
func (d *Den) relaySignal(s *Session, sock *socket, req denproto.RelaySignal) {
	if d.Allow(LimitRelay, strconv.FormatInt(s.MemberID, 10)) != nil {
		return
	}
	if err := denproto.CheckRelaySignal(req); err != nil {
		d.log.Debugf("A box from member %d for their devices was refused: %v", s.MemberID, err)
		return
	}
	subs := d.sockets.devicesOf(s.MemberID, sock.keyID, req.To)
	if len(subs) == 0 {
		return
	}
	e := denproto.RelaySignalEvent{From: sock.keyID, To: req.To, Box: req.Box}
	if err := d.Hub.Ephemeral(denproto.EventRelaySignal, e, func(x *Sub) bool { return subs[x] }); err != nil {
		d.log.Errorf("pass a box between a member's devices: %v", err)
	}
}
