package den

import (
	"container/list"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"golang.org/x/time/rate"
)

// limiter holds a token bucket per key in a fixed-capacity LRU map, so a
// flood of new addresses costs bounded memory: the least recently used
// bucket is evicted when the map is full.
type limiter struct {
	every time.Duration
	burst int
	cap   int

	mu    sync.Mutex
	order *list.List // front is most recently used
	index map[string]*list.Element
}

type bucket struct {
	key string
	lim *rate.Limiter
}

// newLimiter allows burst events per key, refilling one every interval.
func newLimiter(burst int, interval time.Duration, capacity int) *limiter {
	return &limiter{every: interval, burst: burst, cap: capacity, order: list.New(), index: map[string]*list.Element{}}
}

// allow takes a token for key, and reports whether one was available.
func (l *limiter) allow(key string) bool { return l.allowN(key, 1) }

// allowN takes n tokens for key at once, and reports whether there were
// that many; if not, it takes none.
func (l *limiter) allowN(key string, n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.index[key]; ok {
		l.order.MoveToFront(el)
		return el.Value.(*bucket).lim.AllowN(time.Now(), n)
	}
	if l.order.Len() >= l.cap {
		oldest := l.order.Back()
		l.order.Remove(oldest)
		delete(l.index, oldest.Value.(*bucket).key)
	}
	b := &bucket{key: key, lim: rate.NewLimiter(rate.Every(l.every), l.burst)}
	l.index[key] = l.order.PushFront(b)
	return b.lim.AllowN(time.Now(), n)
}

// ipKey keys IPv6 addresses by their /64, so rotating addresses within one
// network share a bucket.
func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// Limit kinds, with the starting values from protocol.md.
const (
	LimitChallenge    = iota // per IP
	LimitJoin                // per IP: join and preview
	LimitLogin               // per IP
	LimitSocket              // per IP: WebSocket upgrades
	LimitWrite               // per member: other writes
	LimitSend                // per member and channel: messages
	LimitTyping              // per member and channel: typing notices
	LimitUpload              // per member: uploads
	LimitPassword            // per IP: password sign-in and recovery
	LimitPasswordName        // per username: anything that checks a password or recovery code
	LimitCall                // per member: joining a call
)

// Allow takes a token from the kind's bucket for key, and returns a
// rate_limited error when none is left.
func (d *Den) Allow(kind int, key string) error {
	var l *limiter
	switch kind {
	case LimitChallenge:
		l = d.limits.challenge
	case LimitJoin:
		l = d.limits.join
	case LimitLogin:
		l = d.limits.login
	case LimitSocket:
		l = d.limits.socket
	case LimitSend:
		l = d.limits.send
	case LimitTyping:
		l = d.limits.typing
	case LimitUpload:
		l = d.limits.upload
	case LimitPassword:
		l = d.limits.password
	case LimitPasswordName:
		l = d.limits.passwordName
	case LimitCall:
		l = d.limits.call
	default:
		l = d.limits.write
	}
	if !l.allow(key) {
		return errRateLimited
	}
	return nil
}

var errRateLimited = denproto.Errorf(http.StatusTooManyRequests, denproto.CodeRateLimited, "slow down")

// AllowMessage takes a message from its author's budget, which holds their
// messages in every channel and DM together, counted one by one and by
// size, the bytes of its text or of a DM's sealed text (M8). Each channel's
// own limit (LimitSend) can't stop a member spreading a flood over many
// channels and DMs, and message text counts against no storage limit, so
// this budget is what bounds how fast one member grows the den's database.
func (d *Den) AllowMessage(member int64, size int) error {
	key := strconv.FormatInt(member, 10)
	if !d.limits.messages.allow(key) || !d.limits.messageBytes.allowN(key, size) {
		return errRateLimited
	}
	return nil
}

// RelaxLimits lifts the rate limits, for development instances, where a
// developer seeds thousands of messages to test the message list.
func (d *Den) RelaxLimits() {
	for _, l := range []**limiter{&d.limits.challenge, &d.limits.join, &d.limits.login, &d.limits.socket, &d.limits.write, &d.limits.send, &d.limits.typing, &d.limits.upload, &d.limits.password, &d.limits.passwordName, &d.limits.call, &d.limits.messages} {
		*l = newLimiter(1_000_000, time.Microsecond, 1000)
	}
	d.limits.messageBytes = newLimiter(1<<40, time.Nanosecond, 1000)
}

// IPKey is the rate-limit key for a client address.
func IPKey(ip net.IP) string { return ipKey(ip) }
