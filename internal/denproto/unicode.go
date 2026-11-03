package denproto

import (
	"strings"
	"sync"
)

// Unicode's emoji (M15.2), from the list scripts/emoji.sh writes, as the
// page's picker has them: the den checks reactions against it (M15.3).

// unicodeEmoji is the list of Unicode's emoji as a set.
var unicodeEmoji = sync.OnceValue(func() map[string]bool {
	set := map[string]bool{}
	for e := range strings.SplitSeq(strings.TrimSuffix(emojiList, "\n"), "\n") {
		set[e] = true
	}
	return set
})

// IsEmoji reports whether s is one of Unicode's emoji, fully qualified, up
// to EmojiVersion: one the page's picker offers, skin tones and all, as the
// list both are made from holds them.
func IsEmoji(s string) bool { return unicodeEmoji()[s] }
