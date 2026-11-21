// Package youtube fetches what the covers of YouTube's players show: a
// linked video's title and channel, with the channel's address, from
// YouTube's oEmbed endpoint, and its picture from i.ytimg.com, a Short's
// portrait, and a larger one for a larger player. The page shows both from
// its own origin, so nothing on it contacts YouTube until a member plays a
// video or opens one of its links.
//
// It asks only at addresses it builds from a checked video ID, follows no
// redirects, caps what it reads, and keeps what it found in memory. It
// sends no cookies, referrer or user agent, and its errors never hold a
// video's ID, which comes from a message and so stays out of logs.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/lookup"
)

const (
	oEmbedURL   = "https://www.youtube.com/oembed"
	picturesURL = "https://i.ytimg.com/vi/"

	maxAnswer  = 64 << 10
	maxPicture = 256 << 10
	// The picture asked for is 480 by 360; much more isn't that picture.
	maxSide = 1280
	// A Short's is 1080 by 1920, and up to about 360 KB.
	maxPortrait     = 1 << 20
	maxPortraitSide = 1920
	// The larger picture is 1280 by 720, and up to about 300 KB.
	maxLarge     = 1 << 20
	maxLargeSide = 1920

	keepFound = time.Hour

	maxTitle   = 200
	maxChannel = 100
	// A channel's address is its handle or ID on YouTube, well short of
	// this; a longer one isn't a channel's.
	maxChannelURL = 200
)

var (
	// ErrBadID is an ID that isn't a YouTube video's; nothing is fetched.
	ErrBadID = errors.New("not a YouTube video's ID")
	// ErrNotFound is a video, or a picture, YouTube doesn't have.
	ErrNotFound = errors.New("YouTube has no such video")
	// ErrUnavailable is YouTube not answering as it should.
	ErrUnavailable = errors.New("YouTube couldn't be reached")
)

// Video is what a cover shows of a video.
type Video struct {
	Title   string `json:"title,omitempty"`
	Channel string `json:"channel,omitempty"`
	// ChannelURL is the channel's address, which the cover links its name
	// to: only an https address on www.youtube.com.
	ChannelURL string `json:"channel_url,omitempty"`
	// Playable is false for a video whose owner turned off embedding, or
	// that's private, which oEmbed refuses to describe.
	Playable bool `json:"playable"`
}

// Lookup fetches covers' details and keeps them. Use New.
type Lookup struct {
	oEmbed, pictures string
	fetch            *lookup.Fetcher
	cache            *lookup.Cache
	now              func() time.Time
}

// New returns a Lookup that asks YouTube.
func New() *Lookup {
	return newLookup(oEmbedURL, picturesURL, lookup.Transport())
}

func newLookup(oEmbed, pictures string, transport http.RoundTripper) *Lookup {
	l := &Lookup{
		oEmbed:   oEmbed,
		pictures: pictures,
		fetch:    lookup.NewFetcher(transport, ErrUnavailable, ErrNotFound),
		now:      time.Now,
	}
	l.cache = lookup.NewCache(ErrNotFound, func() time.Time { return l.now() })
	return l
}

// Video returns what a cover shows of the video id.
func (l *Lookup) Video(ctx context.Context, id string) (Video, error) {
	if !denproto.VideoID(id) {
		return Video{}, ErrBadID
	}
	v, err := l.get(ctx, "video "+id, func(ctx context.Context) (any, int, error) { return l.fetchVideo(ctx, id) })
	if err != nil {
		return Video{}, err
	}
	return v.(Video), nil
}

// Picture returns the video id's picture: a JPEG, with its metadata taken
// out.
func (l *Lookup) Picture(ctx context.Context, id string) ([]byte, error) {
	if !denproto.VideoID(id) {
		return nil, ErrBadID
	}
	v, err := l.get(ctx, "picture "+id, func(ctx context.Context) (any, int, error) {
		return l.fetchPicture(ctx, id, "hqdefault.jpg", maxPicture, maxSide)
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// Large returns the video id's picture at 1280 by 720, for a player wider
// than the usual picture: a JPEG, with its metadata taken out. Where
// YouTube has none, as for an older video, it's the usual picture.
func (l *Lookup) Large(ctx context.Context, id string) ([]byte, error) {
	if !denproto.VideoID(id) {
		return nil, ErrBadID
	}
	v, err := l.get(ctx, "large "+id, func(ctx context.Context) (any, int, error) {
		return l.fetchPicture(ctx, id, "maxresdefault.jpg", maxLarge, maxLargeSide)
	})
	if errors.Is(err, ErrNotFound) {
		return l.Picture(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// Portrait returns a Short's picture, portrait as the Short plays: a JPEG,
// with its metadata taken out. Where YouTube has none, it's the usual
// picture, a frame of the Short between blurred bands, which the page's
// portrait box crops to the frame.
func (l *Lookup) Portrait(ctx context.Context, id string) ([]byte, error) {
	if !denproto.VideoID(id) {
		return nil, ErrBadID
	}
	v, err := l.get(ctx, "portrait "+id, func(ctx context.Context) (any, int, error) {
		return l.fetchPicture(ctx, id, "oar2.jpg", maxPortrait, maxPortraitSide)
	})
	if errors.Is(err, ErrNotFound) {
		return l.Picture(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// get returns what's kept under key, or fetches it once for everyone who
// asks meanwhile.
func (l *Lookup) get(ctx context.Context, key string, fetch func(context.Context) (any, int, error)) (any, error) {
	return l.cache.Get(ctx, key, keepFound, fetch)
}

func (l *Lookup) fetchVideo(ctx context.Context, id string) (any, int, error) {
	q := url.Values{"url": {"https://www.youtube.com/watch?v=" + id}, "format": {"json"}}
	resp, err := l.fetch.Ask(ctx, l.oEmbed+"?"+q.Encode(), "application/json")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// oEmbed refuses a video whose owner turned off embedding, and a
		// private one: either plays only on YouTube, if anywhere.
		return Video{}, 0, nil
	case http.StatusNotFound, http.StatusBadRequest:
		return nil, 0, ErrNotFound
	default:
		return nil, 0, fmt.Errorf("%w: oEmbed answered %d", ErrUnavailable, resp.StatusCode)
	}
	if t, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); t != "application/json" {
		return nil, 0, fmt.Errorf("%w: oEmbed's answer isn't JSON", ErrUnavailable)
	}
	body, err := l.fetch.ReadAll(resp.Body, maxAnswer)
	if err != nil {
		return nil, 0, err
	}
	// The answer also offers a ready-made player, as HTML; nothing but
	// these fields is read.
	var answer struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Author    string `json:"author_name"`
		AuthorURL string `json:"author_url"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Type != "video" {
		return nil, 0, fmt.Errorf("%w: oEmbed's answer doesn't describe a video", ErrUnavailable)
	}
	v := Video{Title: lookup.Clean(answer.Title, maxTitle), Channel: lookup.Clean(answer.Author, maxChannel), Playable: true}
	if v.Channel != "" {
		v.ChannelURL = channelURL(answer.AuthorURL)
	}
	return v, len(v.Title) + len(v.Channel) + len(v.ChannelURL), nil
}

// channelURL returns a channel's address from oEmbed's answer as the page
// may link to it, or "" for anything but an https address on
// www.youtube.com: the link opens in a member's browser, so it goes to
// YouTube or nowhere. It's written out again from its parts, so the page
// gets it in one plain form.
func channelURL(s string) string {
	if len(s) > maxChannelURL {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host != "www.youtube.com" || u.User != nil || u.Opaque != "" {
		return ""
	}
	return u.String()
}

func (l *Lookup) fetchPicture(ctx context.Context, id, name string, maxBytes int64, side int) (any, int, error) {
	jpeg, err := l.fetch.JPEG(ctx, l.pictures+id+"/"+name, maxBytes, side)
	if err != nil {
		return nil, 0, err
	}
	return jpeg, len(jpeg), nil
}
