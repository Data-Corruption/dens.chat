// Package steam fetches what Steam's cards show: a linked game's name,
// developer, description, tags, reviews, price, platforms, Steam Deck's
// rating and release, how many play it now, and its pictures, from Steam's
// store API, which needs no key. The page shows them from its own origin,
// so nothing on it contacts Steam until a member opens one of its links.
//
// It asks only at addresses it builds from a checked app ID, and fetches
// pictures only from Steam's store's own host, at paths inside that app's
// directory; see package lookup for the rest of its rules. The price is
// asked of the store without a country, so Steam prices it for wherever the
// member's IP address puts them, as its store would.
package steam

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/lookup"
)

// Sites are the addresses Steam answers at.
type sites struct {
	items, price, players, tags, assets, video string
}

var steamSites = sites{
	items:   "https://api.steampowered.com/IStoreBrowseService/GetItems/v1/",
	price:   "https://store.steampowered.com/api/appdetails",
	players: "https://api.steampowered.com/ISteamUserStats/GetNumberOfCurrentPlayers/v1/",
	tags:    "https://api.steampowered.com/IStoreService/GetTagList/v1/",
	assets:  "https://shared.akamai.steamstatic.com/store_item_assets/",
	video:   "https://video.akamai.steamstatic.com/store_trailers/",
}

const (
	// An app's answer with trailers can reach tens of KB on titles with
	// many highlights; 1 MiB bounds it safely. The tag list is ~16 KB.
	maxItems   = 1 << 20
	maxTags    = 256 << 10
	maxPrice   = 16 << 10
	maxPlayers = 4 << 10
	// A header is 460 by 215, 920 by 430 at twice the size, and a
	// screenshot's thumbnail 600 by 338, each under 200 KB.
	maxPicture = 1 << 20
	maxSide    = 1920

	keepFound   = time.Hour
	keepPlayers = 10 * time.Minute
	keepTagList = 24 * time.Hour

	maxName        = 200
	maxDeveloper   = 100
	maxDescription = 400
	maxTag         = 40
	maxNote        = 60
	maxPriceText   = 32
	// The card shows three tags, and steps through four screenshots.
	shownTags  = 3
	shownShots = 4
	// More tags than Steam has isn't its list.
	maxTagList = 5000

	// Times past this aren't a release's or a sale's.
	latest = 4102444800 // 2100-01-01
	// More than anyone's count isn't one.
	mostPlayers = 1_000_000_000
	mostReviews = 10_000_000_000
)

var (
	// ErrBadID is an ID that isn't a Steam app's; nothing is fetched.
	ErrBadID = errors.New("not a Steam app's ID")
	// ErrNotFound is an app, or a picture, Steam doesn't have.
	ErrNotFound = errors.New("Steam has no such app")
	// ErrUnavailable is Steam not answering as it should.
	ErrUnavailable = errors.New("Steam couldn't be reached")
)

// Card is what a card shows of an app. A part Steam didn't answer for is
// left out, but for the app itself.
type Card struct {
	Name        string    `json:"name"`
	Kind        string    `json:"kind,omitempty"`
	Developer   string    `json:"developer,omitempty"`
	Description string    `json:"description,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Reviews     *Reviews  `json:"reviews,omitempty"`
	Players     int       `json:"players,omitempty"`
	Platforms   Platforms `json:"platforms"`
	// Deck is Steam Deck's rating: verified, playable or unsupported.
	Deck        string `json:"deck,omitempty"`
	Released    int64  `json:"released,omitempty"`
	ComingSoon  bool   `json:"coming_soon,omitempty"`
	ReleaseNote string `json:"release_note,omitempty"`
	Free        bool   `json:"free,omitempty"`
	Price       *Price `json:"price,omitempty"`
	// Adult is an app Steam marks for adults only, whose pictures the
	// card covers.
	Adult bool `json:"adult,omitempty"`
	// Shots is how many screenshots the card steps through, all for all
	// ages.
	Shots int `json:"shots,omitempty"`
	// Trailer reports whether the app has a primary DASH trailer the member
	// can play on click: all-ages first, or a mature highlight on an adult app
	// whose card stays covered until shown.
	Trailer bool `json:"trailer,omitempty"`
}

// Reviews are an app's reviews as Steam sums them up: its score from 1,
// overwhelmingly negative, to 9, overwhelmingly positive, which the page
// names as Steam does, and the share of them that are positive.
type Reviews struct {
	Score   int   `json:"score"`
	Percent int   `json:"percent"`
	Count   int64 `json:"count"`
}

// Platforms are the systems an app runs on.
type Platforms struct {
	Windows bool `json:"windows"`
	Mac     bool `json:"mac"`
	Linux   bool `json:"linux"`
}

// Price is what an app costs where the member is, as Steam writes it, and
// before its discount while one runs, which ends at Ends, where Steam says.
type Price struct {
	Final    string `json:"final"`
	Original string `json:"original,omitempty"`
	Discount int    `json:"discount,omitempty"`
	Ends     int64  `json:"ends,omitempty"`
}

// Lookup fetches cards' details and keeps them. Use New.
type Lookup struct {
	sites    sites
	fetch    *lookup.Fetcher
	cache    *lookup.Cache
	trailers *lookup.Cache
	now      func() time.Time
}

// New returns a Lookup that asks Steam.
func New() *Lookup {
	return newLookup(steamSites, lookup.Transport())
}

func newLookup(s sites, transport http.RoundTripper) *Lookup {
	l := &Lookup{sites: s, fetch: lookup.NewFetcher(transport, ErrUnavailable, ErrNotFound), now: time.Now}
	now := func() time.Time { return l.now() }
	l.cache = lookup.NewCache(ErrNotFound, now)
	// Trailer manifests and chunks sit in their own 32 MiB cache so
	// streaming video never evicts cards, headers or screenshots.
	l.trailers = lookup.NewCacheWithLimit(ErrNotFound, now, trailerCacheBytes)
	return l
}

// appID reports whether s is a Steam app's ID: digits, without a leading
// zero, up to Steam's largest.
func appID(s string) bool {
	if len(s) == 0 || len(s) > 10 || s[0] == '0' {
		return false
	}
	n, err := strconv.ParseUint(s, 10, 32)
	return err == nil && n > 0
}

// item is what Steam's store says of an app, checked, with the paths of
// its pictures and primary trailer manifests.
type item struct {
	card     Card
	header   string
	large    string
	shots    []string
	trailers map[string]string
	tags     []int
	// sale is the discount the store runs in the US, and when it ends,
	// which the price's discount takes where it's the same.
	sale     int
	saleEnds int64
}

// Card returns what a card shows of the app.
func (l *Lookup) Card(ctx context.Context, app string) (Card, error) {
	it, err := l.item(ctx, app)
	if err != nil {
		return Card{}, err
	}
	c := it.card
	// The price, the players and the tags' names come from three more
	// asks, each of which may fail without failing the card.
	var wg sync.WaitGroup
	var price *Price
	var players int
	var names map[int]string
	wg.Add(3)
	go func() {
		defer wg.Done()
		price, _ = l.price(ctx, app)
	}()
	go func() {
		defer wg.Done()
		players, _ = l.players(ctx, app)
	}()
	go func() {
		defer wg.Done()
		names, _ = l.tagNames(ctx)
	}()
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Card{}, err
	}
	if price != nil {
		p := *price
		if p.Discount > 0 && p.Discount == it.sale {
			p.Ends = it.saleEnds
		}
		c.Price = &p
	}
	c.Players = players
	for _, id := range it.tags {
		if name := names[id]; name != "" && len(c.Tags) < shownTags {
			c.Tags = append(c.Tags, name)
		}
	}
	return c, nil
}

// Header returns the app's header picture, at twice the size where Steam
// has one: a JPEG, with its metadata taken out.
func (l *Lookup) Header(ctx context.Context, app string) ([]byte, error) {
	it, err := l.item(ctx, app)
	if err != nil {
		return nil, err
	}
	if it.large != "" {
		b, err := l.picture(ctx, it.large)
		if !errors.Is(err, ErrNotFound) {
			return b, err
		}
	}
	if it.header == "" {
		return nil, ErrNotFound
	}
	return l.picture(ctx, it.header)
}

// Shot returns the app's nth screenshot, from 1, at the size of Steam's
// thumbnails: a JPEG, with its metadata taken out.
func (l *Lookup) Shot(ctx context.Context, app string, n int) ([]byte, error) {
	it, err := l.item(ctx, app)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > len(it.shots) {
		return nil, ErrNotFound
	}
	return l.picture(ctx, it.shots[n-1])
}

func (l *Lookup) item(ctx context.Context, app string) (item, error) {
	if !appID(app) {
		return item{}, ErrBadID
	}
	v, err := l.cache.Get(ctx, "item "+app, keepFound, func(ctx context.Context) (any, int, error) { return l.fetchItem(ctx, app) })
	if err != nil {
		return item{}, err
	}
	return v.(item), nil
}

func (l *Lookup) price(ctx context.Context, app string) (*Price, error) {
	v, err := l.cache.Get(ctx, "price "+app, keepFound, func(ctx context.Context) (any, int, error) { return l.fetchPrice(ctx, app) })
	if err != nil {
		return nil, err
	}
	return v.(*Price), nil
}

func (l *Lookup) players(ctx context.Context, app string) (int, error) {
	v, err := l.cache.Get(ctx, "players "+app, keepPlayers, func(ctx context.Context) (any, int, error) { return l.fetchPlayers(ctx, app) })
	if err != nil {
		return 0, err
	}
	return v.(int), nil
}

func (l *Lookup) tagNames(ctx context.Context) (map[int]string, error) {
	v, err := l.cache.Get(ctx, "tags", keepTagList, l.fetchTags)
	if err != nil {
		return nil, err
	}
	return v.(map[int]string), nil
}

func (l *Lookup) picture(ctx context.Context, path string) ([]byte, error) {
	v, err := l.cache.Get(ctx, "picture "+path, keepFound, func(ctx context.Context) (any, int, error) {
		b, err := l.fetch.JPEG(ctx, l.sites.assets+path, maxPicture, maxSide)
		return b, len(b), err
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// askJSON asks Steam for a JSON answer of at most max bytes; a status
// other than 200 fails as unavailable.
func (l *Lookup) askJSON(ctx context.Context, address string, max int64) ([]byte, error) {
	resp, err := l.fetch.Ask(ctx, address, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: Steam answered %d", ErrUnavailable, resp.StatusCode)
	}
	if t, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); t != "application/json" {
		return nil, fmt.Errorf("%w: Steam's answer isn't JSON", ErrUnavailable)
	}
	return l.fetch.ReadAll(resp.Body, max)
}

// storeItem is what's read of GetItems' answer for an app; nothing else
// in it is.
type storeItem struct {
	ID        uint32 `json:"id"`
	Success   int    `json:"success"`
	Name      string `json:"name"`
	Type      *int   `json:"type"`
	IsFree    bool   `json:"is_free"`
	Coming    bool   `json:"is_coming_soon"`
	BasicInfo struct {
		Description string `json:"short_description"`
		Developers  []struct {
			Name string `json:"name"`
		} `json:"developers"`
	} `json:"basic_info"`
	Tags    []storeTag `json:"tags"`
	Reviews struct {
		Summary struct {
			Count   int64 `json:"review_count"`
			Percent int   `json:"percent_positive"`
			Score   int   `json:"review_score"`
		} `json:"summary_filtered"`
	} `json:"reviews"`
	Assets struct {
		Format   string `json:"asset_url_format"`
		Header   string `json:"header"`
		Header2x string `json:"header_2x"`
	} `json:"assets"`
	Release struct {
		Date   int64  `json:"steam_release_date"`
		Coming bool   `json:"is_coming_soon"`
		Note   string `json:"custom_release_date_message"`
	} `json:"release"`
	Platforms struct {
		Windows bool `json:"windows"`
		Mac     bool `json:"mac"`
		Linux   bool `json:"steamos_linux"`
		Deck    int  `json:"steam_deck_compat_category"`
	} `json:"platforms"`
	Purchase struct {
		Discount  int `json:"discount_pct"`
		Discounts []struct {
			Ends int64 `json:"discount_end_date"`
		} `json:"active_discounts"`
	} `json:"best_purchase_option"`
	Descriptors []int `json:"content_descriptorids"`
	Screenshots struct {
		AllAges []struct {
			Filename string `json:"filename"`
		} `json:"all_ages_screenshots"`
	} `json:"screenshots"`
	Trailers struct {
		Highlights []storeTrailer `json:"highlights"`
		Other      []storeTrailer `json:"other_trailers"`
	} `json:"trailers"`
}

type storeTrailer struct {
	AllAges  bool `json:"all_ages"`
	Adaptive []struct {
		Encoding string `json:"encoding"`
		CDNPath  string `json:"cdn_path"`
	} `json:"adaptive_trailers"`
}

type storeTag struct {
	ID     int `json:"tagid"`
	Weight int `json:"weight"`
}

// kinds names the kinds of app GetItems' types are, where Steam's store
// has shown which.
var kinds = map[int]string{0: "game", 4: "dlc", 6: "software", 11: "soundtrack"}

// decks names Steam Deck's ratings.
var decks = map[int]string{1: "unsupported", 2: "playable", 3: "verified"}

func (l *Lookup) fetchItem(ctx context.Context, app string) (any, int, error) {
	// The store answers nothing without a country; the price comes from
	// appdetails, which prices for the member's.
	input, _ := json.Marshal(map[string]any{
		"ids":     []map[string]any{{"appid": json.Number(app)}},
		"context": map[string]string{"language": "english", "country_code": "US"},
		"data_request": map[string]any{
			"include_assets": true, "include_release": true, "include_platforms": true, "include_reviews": true,
			"include_basic_info": true, "include_tag_count": 5, "include_ratings": true, "include_screenshots": true,
			"include_trailers": true,
		},
	})
	body, err := l.askJSON(ctx, l.sites.items+"?"+url.Values{"input_json": {string(input)}}.Encode(), maxItems)
	if err != nil {
		return nil, 0, err
	}
	var answer struct {
		Response struct {
			Items []storeItem `json:"store_items"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || len(answer.Response.Items) != 1 {
		return nil, 0, fmt.Errorf("%w: Steam's answer doesn't describe an app", ErrUnavailable)
	}
	s := answer.Response.Items[0]
	if strconv.FormatUint(uint64(s.ID), 10) != app {
		return nil, 0, fmt.Errorf("%w: Steam's answer describes another app", ErrUnavailable)
	}
	if s.Success != 1 {
		return nil, 0, ErrNotFound
	}
	it, err := checkItem(app, s)
	if err != nil {
		return nil, 0, err
	}
	size := len(it.card.Name) + len(it.card.Developer) + len(it.card.Description) + len(it.card.ReleaseNote) + len(it.header) + len(it.large) + 8*len(it.tags)
	for _, p := range it.shots {
		size += len(p)
	}
	for k, p := range it.trailers {
		size += len(k) + len(p)
	}
	return it, size, nil
}

// checkItem makes a card's details from what the store said of app,
// keeping only what reads as what it should be.
func checkItem(app string, s storeItem) (item, error) {
	c := Card{
		Name:        lookup.Clean(s.Name, maxName),
		Description: lookup.Clean(html.UnescapeString(s.BasicInfo.Description), maxDescription),
		Platforms:   Platforms{Windows: s.Platforms.Windows, Mac: s.Platforms.Mac, Linux: s.Platforms.Linux},
		Deck:        decks[s.Platforms.Deck],
		ComingSoon:  s.Coming || s.Release.Coming,
		ReleaseNote: lookup.Clean(s.Release.Note, maxNote),
		Free:        s.IsFree,
	}
	if c.Name == "" {
		return item{}, fmt.Errorf("%w: Steam's answer names no app", ErrUnavailable)
	}
	if s.Type != nil {
		c.Kind = kinds[*s.Type]
	}
	if len(s.BasicInfo.Developers) > 0 {
		c.Developer = lookup.Clean(s.BasicInfo.Developers[0].Name, maxDeveloper)
	}
	if r := s.Reviews.Summary; r.Count > 0 && r.Count <= mostReviews && r.Score >= 1 && r.Score <= 9 && r.Percent >= 0 && r.Percent <= 100 {
		c.Reviews = &Reviews{Score: r.Score, Percent: r.Percent, Count: r.Count}
	}
	if d := s.Release.Date; d > 0 && d < latest {
		c.Released = d
	}
	c.Adult = slices.Contains(s.Descriptors, 3) || slices.Contains(s.Descriptors, 4)
	it := item{}
	if s.Purchase.Discount > 0 && s.Purchase.Discount <= 100 && len(s.Purchase.Discounts) > 0 {
		if e := s.Purchase.Discounts[0].Ends; e > 0 && e < latest {
			it.sale, it.saleEnds = s.Purchase.Discount, e
		}
	}
	// The tags heaviest first, as the store lists them.
	tags := slices.Clone(s.Tags)
	slices.SortStableFunc(tags, func(a, b storeTag) int { return cmp.Compare(b.Weight, a.Weight) })
	for _, t := range tags {
		if t.ID > 0 && len(it.tags) < 5 {
			it.tags = append(it.tags, t.ID)
		}
	}
	it.header = assetPath(app, s.Assets.Format, s.Assets.Header)
	it.large = assetPath(app, s.Assets.Format, s.Assets.Header2x)
	for _, shot := range s.Screenshots.AllAges {
		if len(it.shots) == shownShots {
			break
		}
		if p := thumbnail(app, shot.Filename); p != "" {
			it.shots = append(it.shots, p)
		}
	}
	c.Shots = len(it.shots)
	trailerPaths := func(h storeTrailer) map[string]string {
		paths := map[string]string{}
		for _, a := range h.Adaptive {
			var enc string
			switch a.Encoding {
			case "dash_h264":
				enc = "h264"
			case "dash_av1":
				enc = "av1"
			default:
				continue
			}
			if p := trailerPath(app, enc, a.CDNPath); p != "" {
				paths[enc] = p
			}
		}
		return paths
	}
	for _, h := range s.Trailers.Highlights {
		if !h.AllAges {
			continue
		}
		if paths := trailerPaths(h); len(paths) > 0 {
			it.trailers = paths
			break
		}
	}
	// An app already behind the adults-only cover may mark all of its
	// trailers as not all-ages; fall back to its first valid trailer only
	// when the card is covered.
	if len(it.trailers) == 0 && c.Adult {
		for _, list := range [][]storeTrailer{s.Trailers.Highlights, s.Trailers.Other} {
			for _, h := range list {
				if paths := trailerPaths(h); len(paths) > 0 {
					it.trailers = paths
					break
				}
			}
			if len(it.trailers) > 0 {
				break
			}
		}
	}
	c.Trailer = len(it.trailers) > 0
	it.card = c
	return it, nil
}

// segment is one part of a picture's path on Steam's store.
var segment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}$`)

// picturePath returns a picture's path, as Steam's store gives it, if it's
// a JPEG's inside the app's directory, with nothing but its time after
// it; or "".
func picturePath(app, p string) string {
	path, query, _ := strings.Cut(p, "?")
	if query != "" {
		t, ok := strings.CutPrefix(query, "t=")
		if !ok || len(t) == 0 || len(t) > 12 || strings.Trim(t, "0123456789") != "" {
			return ""
		}
	}
	rest, ok := strings.CutPrefix(path, "steam/apps/"+app+"/")
	if !ok || !strings.HasSuffix(rest, ".jpg") {
		return ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) > 2 {
		return ""
	}
	for _, s := range parts {
		if !segment.MatchString(s) || strings.Contains(s, "..") {
			return ""
		}
	}
	return p
}

// assetPath returns where one of the app's pictures is, from the format
// the store gives its pictures' addresses in and the picture's name, or
// "".
func assetPath(app, format, name string) string {
	if name == "" || strings.Count(format, "${FILENAME}") != 1 {
		return ""
	}
	return picturePath(app, strings.Replace(format, "${FILENAME}", name, 1))
}

// thumbnail returns where a screenshot's thumbnail is, from where the
// store says the screenshot is, or "".
func thumbnail(app, filename string) string {
	p := picturePath(app, filename)
	if p == "" {
		return ""
	}
	path, query, _ := strings.Cut(p, "?")
	out := strings.TrimSuffix(path, ".jpg") + ".600x338.jpg"
	if query != "" {
		out += "?" + query
	}
	return out
}

func (l *Lookup) fetchPrice(ctx context.Context, app string) (any, int, error) {
	q := url.Values{"appids": {app}, "filters": {"price_overview"}}
	body, err := l.askJSON(ctx, l.sites.price+"?"+q.Encode(), maxPrice)
	if err != nil {
		return nil, 0, err
	}
	var answer map[string]struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, 0, fmt.Errorf("%w: Steam's price isn't one", ErrUnavailable)
	}
	a, ok := answer[app]
	if !ok || !a.Success {
		return nil, 0, ErrNotFound
	}
	// A free app's data is an empty list.
	var data struct {
		Price *struct {
			Discount int    `json:"discount_percent"`
			Initial  string `json:"initial_formatted"`
			Final    string `json:"final_formatted"`
		} `json:"price_overview"`
	}
	if len(a.Data) == 0 || a.Data[0] != '{' {
		return (*Price)(nil), 0, nil
	}
	if err := json.Unmarshal(a.Data, &data); err != nil {
		return nil, 0, fmt.Errorf("%w: Steam's price isn't one", ErrUnavailable)
	}
	if data.Price == nil {
		return (*Price)(nil), 0, nil
	}
	p := &Price{Final: lookup.Clean(data.Price.Final, maxPriceText)}
	if p.Final == "" {
		return (*Price)(nil), 0, nil
	}
	if d := data.Price.Discount; d > 0 && d <= 100 {
		p.Discount, p.Original = d, lookup.Clean(data.Price.Initial, maxPriceText)
	}
	return p, len(p.Final) + len(p.Original) + 16, nil
}

func (l *Lookup) fetchPlayers(ctx context.Context, app string) (any, int, error) {
	body, err := l.askJSON(ctx, l.sites.players+"?"+url.Values{"appid": {app}}.Encode(), maxPlayers)
	if err != nil {
		return nil, 0, err
	}
	var answer struct {
		Response struct {
			Count  int64 `json:"player_count"`
			Result int   `json:"result"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, 0, fmt.Errorf("%w: Steam's count isn't one", ErrUnavailable)
	}
	// Any other result says the app counts nobody, as an unreleased one.
	r := answer.Response
	if r.Result != 1 || r.Count < 0 || r.Count > mostPlayers {
		return 0, 8, nil
	}
	return int(r.Count), 8, nil
}

func (l *Lookup) fetchTags(ctx context.Context) (any, int, error) {
	body, err := l.askJSON(ctx, l.sites.tags+"?"+url.Values{"language": {"english"}}.Encode(), maxTags)
	if err != nil {
		return nil, 0, err
	}
	var answer struct {
		Response struct {
			Tags []struct {
				ID   int    `json:"tagid"`
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || len(answer.Response.Tags) > maxTagList {
		return nil, 0, fmt.Errorf("%w: Steam's tags aren't a list of them", ErrUnavailable)
	}
	names := make(map[int]string, len(answer.Response.Tags))
	size := 0
	for _, t := range answer.Response.Tags {
		if name := lookup.Clean(t.Name, maxTag); t.ID > 0 && name != "" {
			names[t.ID] = name
			size += len(name) + 16
		}
	}
	return names, size, nil
}
