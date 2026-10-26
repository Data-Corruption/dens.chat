package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A call (M2) is between the page that joined it and a den's SFU. This
// install holds one call at a time, for one page, and relays its signaling
// between the den's socket and that page's event stream. It also decides
// where the page's browser sends media: where the install reaches the den,
// so the den can't point the browser anywhere else.

// CallDisconnected ends a call whose den connection dropped. The den
// heard the socket close and ended the call itself.
const CallDisconnected = "disconnected"

// CallEvent goes to the page that holds the install's call: an offer to
// answer, carrying the den's candidates; that the den took the call back
// after its connection dropped (M3); that it refused a share or a watch
// (M4.2); who in the call holds push to talk (M11); or the call's end and
// why.
type CallEvent struct {
	DenID   string       `json:"den"`
	Channel string       `json:"channel"`
	Offer   *CallOffer   `json:"offer,omitempty"`
	Resumed bool         `json:"resumed,omitempty"`
	Refused *CallRefusal `json:"refused,omitempty"`
	Talking *[]string    `json:"talking,omitempty"`
	Ended   string       `json:"ended,omitempty"`
}

// CallRefusal is the den refusing the page's share, or its watch of a
// member's share, and why.
type CallRefusal struct {
	What     string `json:"what"`
	MemberID string `json:"member_id,omitempty"`
	Reason   string `json:"reason"`
}

// CallOffer carries the den's media ports beside its SDP, so the page can
// name them when a call can't connect.
type CallOffer struct {
	Version int    `json:"version"`
	SDP     string `json:"sdp"`
	UDPPort int    `json:"udp_port"`
	TCPPort int    `json:"tcp_port"`
}

type activeCall struct {
	page, den, channel string
	deliver            func(CallEvent)
	addrs              []netip.Addr // where the browser reaches the den's media ports
	// For the log (M11): the den's ID as the page shows it; what the page
	// last asked of the den that an offer or a resume answers, a join, a
	// resume or a restart, and when; and when the last offer went to the
	// page.
	fp        string
	asked     string
	askedAt   time.Time
	offeredAt time.Time
}

// note logs a step of a call's signaling, at debug (M11).
func (m *Manager) note(call *activeCall, format string, args ...any) {
	m.log.Debugf("Den %s, call in channel %s: "+format, append([]any{call.fp, call.channel}, args...)...)
}

// since says how long ago t was, for the log.
func since(t time.Time) time.Duration { return time.Since(t).Round(time.Millisecond) }

// JoinCall starts the install's call in one of a den's voice channels, for
// a page, which gets the call's offers and its end through deliver; it
// mustn't block. A call another page held ends, as moved. With resume, the
// page asks the den for the call it holds for this device after the den's
// connection dropped (M3), which the page's peer connection carries on.
// Either way, the den's addresses are looked up afresh, since the network
// may have changed.
func (m *Manager) JoinCall(ctx context.Context, page, denID, channel string, marks denproto.VoiceMute, resume bool, deliver func(CallEvent)) error {
	if !validID(channel) {
		return inputError(errors.New("no such channel"))
	}
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	asked := "join"
	if resume {
		asked = "resume"
	}
	call := &activeCall{page: page, den: denID, channel: channel, deliver: deliver, fp: denproto.Fingerprint(c.j.denID), asked: asked}
	end := func(reason string) { deliver(CallEvent{DenID: denID, Channel: channel, Ended: reason}) }
	if call.addrs, err = c.mediaAddrs(ctx); err != nil {
		m.log.Warnf("Couldn't find where to send a call's media: %v", logged(err))
		end(denproto.VoiceFailed)
		return nil
	}
	call.askedAt = time.Now()
	m.callMu.Lock()
	old := m.call
	m.call = call
	m.callMu.Unlock()
	m.callChanged()
	if old != nil {
		if old.page != page {
			m.note(old, "moved to another page")
			old.deliver(CallEvent{DenID: old.den, Channel: old.channel, Ended: denproto.VoiceMoved})
		}
		// A den hangs up a member's old call when they join another;
		// another den has to be told.
		if old.den != denID {
			if oc, err := m.find(old.den); err == nil {
				_ = oc.sendFrame(denproto.EventVoiceLeave, struct{}{})
			}
		}
	}
	join := denproto.VoiceJoin{ChannelID: channel, Muted: marks.Muted, Deafened: marks.Deafened, Resume: resume}
	if err := c.sendFrame(denproto.EventVoiceJoin, join); err != nil {
		m.note(call, "couldn't ask to %s it: the den isn't connected", asked)
		if m.takeCall(func(x *activeCall) bool { return x == call }) != nil {
			end(CallDisconnected)
		}
		return nil
	}
	m.note(call, "asked to %s it", asked)
	return nil
}

// AnswerCall passes the page's answer to the den, without the browser's
// candidates: the den answers checks from wherever they come, and needn't
// learn this computer's addresses.
func (m *Manager) AnswerCall(page, denID string, version int, sdp string) {
	call := m.currentCall(page, denID)
	if call == nil {
		return
	}
	sdp = denproto.StripCandidates(sdp)
	if len(sdp) > denproto.MaxSDP {
		m.failCall(call, "the page's answer was too large")
		return
	}
	if c, err := m.find(denID); err == nil && c.sendFrame(denproto.EventVoiceAnswer, denproto.VoiceAnswer{Version: version, SDP: sdp}) == nil {
		m.callMu.Lock()
		offered := call.offeredAt
		m.callMu.Unlock()
		m.note(call, "answer %d went, %s after its offer", version, since(offered))
	}
}

// RestartCall has the den restart the ICE of the page's call, as when the
// browser's connection to the media ports broke (M3). The den's address
// may have changed with the network, so the offer that follows gets it
// afresh.
func (m *Manager) RestartCall(ctx context.Context, page, denID string) {
	call := m.currentCall(page, denID)
	if call == nil {
		return
	}
	c, err := m.find(denID)
	if err != nil {
		return
	}
	addrs, err := c.mediaAddrs(ctx)
	m.callMu.Lock()
	if err == nil {
		call.addrs = addrs
	}
	call.asked, call.askedAt = "restart", time.Now()
	m.callMu.Unlock()
	if c.sendFrame(denproto.EventVoiceRestart, struct{}{}) == nil {
		m.note(call, "asked to restart ICE")
	}
}

// MuteCall sets the member's marks in the page's call: muted, and
// deafened.
func (m *Manager) MuteCall(page, denID string, marks denproto.VoiceMute) {
	if m.currentCall(page, denID) == nil {
		return
	}
	if c, err := m.find(denID); err == nil {
		_ = c.sendFrame(denproto.EventVoiceMute, marks)
	}
}

// ShareCall starts or ends the share of the page's call (M4.2), with its
// sound if sound.
func (m *Manager) ShareCall(page, denID string, on, sound bool) {
	if m.currentCall(page, denID) == nil {
		return
	}
	if c, err := m.find(denID); err == nil {
		_ = c.sendFrame(denproto.EventVoiceShare, denproto.VoiceShare{On: on, Sound: on && sound})
	}
}

// TalkCall tells the den whether the page's member holds push to talk
// (M11).
func (m *Manager) TalkCall(page, denID string, on bool) {
	if m.currentCall(page, denID) == nil {
		return
	}
	if c, err := m.find(denID); err == nil {
		_ = c.sendFrame(denproto.EventVoiceTalk, denproto.VoiceTalk{On: on})
	}
}

// WatchCall starts or stops the page's call watching a member's share
// (M4.2).
func (m *Manager) WatchCall(page, denID, member string, on bool) {
	if m.currentCall(page, denID) == nil || !validID(member) {
		return
	}
	if c, err := m.find(denID); err == nil {
		_ = c.sendFrame(denproto.EventVoiceWatch, denproto.VoiceWatch{MemberID: member, On: on})
	}
}

// LeaveCall ends the page's call.
func (m *Manager) LeaveCall(page, denID string) {
	call := m.takeCall(func(x *activeCall) bool { return x.page == page && x.den == denID })
	if call == nil {
		return
	}
	m.note(call, "the page left it")
	if c, err := m.find(denID); err == nil {
		_ = c.sendFrame(denproto.EventVoiceLeave, struct{}{})
	}
}

// DropPage ends the call a page held, once its stream has closed, which
// goes to the log at info (M11): a page whose stream drops while its tab
// stays open loses its call this way.
func (m *Manager) DropPage(page string) {
	call := m.takeCall(func(x *activeCall) bool { return x.page == page })
	if call == nil {
		return
	}
	m.log.Infof("Den %s, call in channel %s: ended, as the stream of page %s, which held it, closed", call.fp, call.channel, page)
	if c, err := m.find(call.den); err == nil {
		_ = c.sendFrame(denproto.EventVoiceLeave, struct{}{})
	}
}

func (m *Manager) currentCall(page, denID string) *activeCall {
	m.callMu.Lock()
	defer m.callMu.Unlock()
	if m.call == nil || m.call.page != page || m.call.den != denID {
		return nil
	}
	return m.call
}

// takeCall clears the install's call if it matches, and returns it.
func (m *Manager) takeCall(match func(*activeCall) bool) *activeCall {
	m.callMu.Lock()
	call := m.call
	if call == nil || !match(call) {
		m.callMu.Unlock()
		return nil
	}
	m.call = nil
	m.callMu.Unlock()
	m.callChanged()
	return call
}

// callChanged tells OnCall whether the install holds a call now.
func (m *Manager) callChanged() {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.callMu.Lock()
	active := m.call != nil
	m.callMu.Unlock()
	if m.OnCall != nil {
		m.OnCall(active)
	}
}

// failCall hangs up a call this install can't go on with, and tells its
// page.
func (m *Manager) failCall(call *activeCall, why string) {
	if m.takeCall(func(x *activeCall) bool { return x == call }) == nil {
		return
	}
	m.note(call, "ended, as %s", why)
	if c, err := m.find(call.den); err == nil {
		_ = c.sendFrame(denproto.EventVoiceLeave, struct{}{})
	}
	call.deliver(CallEvent{DenID: call.den, Channel: call.channel, Ended: denproto.VoiceFailed})
}

// denDropped ends the call on a den whose connection dropped.
func (m *Manager) denDropped(denID string) {
	if call := m.takeCall(func(x *activeCall) bool { return x.den == denID }); call != nil {
		m.note(call, "the den's connection dropped, so the page holds it")
		call.deliver(CallEvent{DenID: call.den, Channel: call.channel, Ended: CallDisconnected})
	}
}

// callEvent handles a den's offer or end of a call. An offer goes to the
// page that holds the call once it checks out, with the den's candidates
// written in; an offer that doesn't ends the call, since only it is at
// stake.
func (c *conn) callEvent(e denproto.Event) error {
	m := c.m
	denID := c.j.denID.String()
	switch e.T {
	case denproto.EventVoiceOffer:
		var o denproto.VoiceOffer
		if json.Unmarshal(e.D, &o) != nil || o.Version < 1 {
			return errMalformed
		}
		m.callMu.Lock()
		call := m.call
		var addrs []netip.Addr
		after := ""
		if call != nil && call.den == denID && call.channel == o.ChannelID {
			addrs = call.addrs
			// An offer answers the join, resume or restart before it, or
			// comes as the call changes.
			if call.asked != "" {
				after = ", " + since(call.askedAt).String() + " after the " + call.asked
				call.asked = ""
			}
			call.offeredAt = time.Now()
		}
		m.callMu.Unlock()
		if call == nil || call.den != denID || call.channel != o.ChannelID {
			return nil
		}
		sdp, err := denproto.AddCandidates(denproto.StripCandidates(o.SDP), addrs, o.UDPPort, o.TCPPort)
		if err == nil {
			err = denproto.CheckOffer(sdp)
		}
		if err != nil {
			m.log.Warnf("A den's offer for a call was refused: %v", err)
			m.failCall(call, "its offer didn't check out")
			return nil
		}
		m.note(call, "offer %d came%s", o.Version, after)
		call.deliver(CallEvent{DenID: denID, Channel: o.ChannelID,
			Offer: &CallOffer{Version: o.Version, SDP: sdp, UDPPort: o.UDPPort, TCPPort: o.TCPPort}})
	case denproto.EventVoiceResumed:
		var x denproto.VoiceResumed
		if json.Unmarshal(e.D, &x) != nil {
			return errMalformed
		}
		m.callMu.Lock()
		call := m.call
		var asked time.Time
		if call != nil && call.den == denID && call.channel == x.ChannelID {
			asked = call.askedAt
			call.asked = ""
		}
		m.callMu.Unlock()
		if call != nil && call.den == denID && call.channel == x.ChannelID {
			m.note(call, "the den took it back, %s after the resume", since(asked))
			call.deliver(CallEvent{DenID: denID, Channel: x.ChannelID, Resumed: true})
		}
	case denproto.EventVoiceRefused:
		var x denproto.VoiceRefused
		if json.Unmarshal(e.D, &x) != nil {
			return errMalformed
		}
		refusal, ok := cleanRefusal(x)
		if !ok {
			return errMalformed
		}
		m.callMu.Lock()
		call := m.call
		m.callMu.Unlock()
		if call != nil && call.den == denID && call.channel == x.ChannelID {
			m.note(call, "the den refused the page's %s: %s", refusal.What, refusal.Reason)
			call.deliver(CallEvent{DenID: denID, Channel: x.ChannelID, Refused: &refusal})
		}
	case denproto.EventVoiceTalking:
		var x denproto.VoiceTalking
		if json.Unmarshal(e.D, &x) != nil || len(x.Members) > denproto.MaxCallMembers {
			return errMalformed
		}
		for _, id := range x.Members {
			if !validID(id) {
				return errMalformed
			}
		}
		m.callMu.Lock()
		call := m.call
		m.callMu.Unlock()
		if call != nil && call.den == denID && call.channel == x.ChannelID {
			talking := append([]string{}, x.Members...)
			call.deliver(CallEvent{DenID: denID, Channel: x.ChannelID, Talking: &talking})
		}
	case denproto.EventVoiceEnded:
		var x denproto.VoiceEnded
		if json.Unmarshal(e.D, &x) != nil {
			return errMalformed
		}
		call := m.takeCall(func(a *activeCall) bool { return a.den == denID && a.channel == x.ChannelID })
		if call != nil {
			reason := cleanReason(x.Reason)
			m.note(call, "the den ended it: %s", reason)
			call.deliver(CallEvent{DenID: denID, Channel: x.ChannelID, Ended: reason})
		}
	}
	return nil
}

// cleanRefusal checks a den's refusal of a share or a watch, and passes on
// a reason the page knows, calling any other a failure.
func cleanRefusal(x denproto.VoiceRefused) (CallRefusal, bool) {
	r := CallRefusal{What: x.What, MemberID: x.MemberID, Reason: x.Reason}
	switch {
	case x.What == denproto.RefusedShare && x.MemberID == "":
	case x.What == denproto.RefusedWatch && validID(x.MemberID):
	default:
		return r, false
	}
	switch x.Reason {
	case denproto.RefusedOff, denproto.RefusedFull, denproto.RefusedStaffMuted, denproto.RefusedNotSharing, denproto.VoiceRateLimited:
	default:
		r.Reason = denproto.VoiceFailed
	}
	return r, true
}

// cleanReason passes on a reason the page knows, and calls any other a
// failure: the reason comes from a den.
func cleanReason(reason string) string {
	switch reason {
	case denproto.VoiceMoved, denproto.VoiceNotFound, denproto.VoiceFull, denproto.VoiceRateLimited,
		denproto.VoiceForbidden, denproto.VoiceDeleted, denproto.VoiceDisconnectedByStaff:
		return reason
	}
	return denproto.VoiceFailed
}

// mediaAddrs are where this install's browser sends a den's media: each
// address the den's name resolves to, where the install reaches it. For a
// den on this machine, its own or one whose name resolves to loopback, it's
// the machine's addresses on its other interfaces, since browsers don't use
// loopback for WebRTC.
func (c *conn) mediaAddrs(ctx context.Context) ([]netip.Addr, error) {
	a := c.remote()
	return c.m.mediaAddrs(ctx, a.base, a.own)
}

func (m *Manager) mediaAddrs(ctx context.Context, base string, own bool) ([]netip.Addr, error) {
	if own {
		return m.LocalAddrs()
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	var found []netip.Addr
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		found = []netip.Addr{ip}
	} else if found, err = m.LookupHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	var addrs []netip.Addr
	for _, ip := range found {
		ip = ip.Unmap()
		if ip.IsLoopback() {
			return m.LocalAddrs()
		}
		if ip.IsGlobalUnicast() && !slices.Contains(addrs, ip) {
			addrs = append(addrs, ip)
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("the den's name resolves to no address a browser can reach")
	}
	return addrs, nil
}

func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// localAddrs lists this machine's addresses on interfaces that are up,
// other than loopback: IPv4 first, without IPv6's link-local ones.
func localAddrs() ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var v4, v6 []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP)
			if !ok || !ip.Unmap().IsGlobalUnicast() {
				continue
			}
			ip = ip.Unmap()
			if ip.Is4() {
				v4 = append(v4, ip)
			} else {
				v6 = append(v6, ip)
			}
		}
	}
	if all := append(v4, v6...); len(all) > 0 {
		return all, nil
	}
	return nil, errors.New("this computer has no network address besides loopback")
}
