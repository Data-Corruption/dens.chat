package steam

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// Trailer manifests and chunks sit in their own 32 MiB short-TTL cache
	// so streaming video never evicts cards, headers or screenshots.
	trailerCacheBytes  = 32 << 20
	keepTrailer        = 10 * time.Minute
	maxManifest        = 64 << 10
	maxSegment         = 4 << 20
	maxTrailerSegments = 1000
	maxTrailerReps     = 16
	maxTrailerDuration = 3600.0
	maxSegmentDuration = 60.0
)

// Trailer describes an app's primary all-ages trailer for one DASH encoding
// ("h264" or "av1"), with no raw CDN paths.
type Trailer struct {
	Encoding string         `json:"encoding"`
	Duration float64        `json:"duration"`
	Video    []TrailerTrack `json:"video"`
	Audio    *TrailerTrack  `json:"audio,omitempty"`
}

// TrailerTrack is one video or audio rendition the page may fetch segments for.
type TrailerTrack struct {
	ID              string  `json:"id"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	Mime            string  `json:"mime"`
	Codecs          string  `json:"codecs"`
	Segments        int     `json:"segments"`
	SegmentDuration float64 `json:"segment_duration"`
}

type parsedManifest struct {
	trailer Trailer
	baseDir string
	tracks  map[string]manifestTrack
}

type manifestTrack struct {
	mime        string
	initPath    string
	mediaTmpl   string
	startNumber int
	segments    int
}

var (
	trailerCDNPathRe  = regexp.MustCompile(`^[1-9][0-9]{0,9}/[0-9]{1,12}/[0-9a-f]{40}/[0-9]{1,12}/dash_(h264|av1)\.mpd$`)
	trailerRepIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,16}$`)
	trailerCodecsRe   = regexp.MustCompile(`^[A-Za-z0-9._,-]{1,64}$`)
	trailerInitRelRe  = regexp.MustCompile(`^dash_(h264|av1)/init-stream[A-Za-z0-9_-]{1,16}\.m4s$`)
	trailerChunkRelRe = regexp.MustCompile(`^dash_(h264|av1)/chunk-stream[A-Za-z0-9_-]{1,16}-[0-9]{5}\.m4s$`)
	isoDurationRe     = regexp.MustCompile(`^PT(?:([0-9]+(?:\.[0-9]+)?)H)?(?:([0-9]+(?:\.[0-9]+)?)M)?(?:([0-9]+(?:\.[0-9]+)?)S)?$`)
)

func validEncoding(enc string) bool {
	return enc == "h264" || enc == "av1"
}

// trailerPath returns Steam's DASH manifest path if it stays strictly inside
// app's store_trailers directory for enc, or "".
func trailerPath(app, enc, p string) string {
	if !validEncoding(enc) {
		return ""
	}
	if !strings.HasPrefix(p, app+"/") || !strings.HasSuffix(p, "/dash_"+enc+".mpd") {
		return ""
	}
	if !trailerCDNPathRe.MatchString(p) {
		return ""
	}
	return p
}

// Trailer returns the parsed DASH track descriptor for the app's primary
// all-ages trailer in encoding ("h264" or "av1").
func (l *Lookup) Trailer(ctx context.Context, app, encoding string) (Trailer, error) {
	m, err := l.trailerManifest(ctx, app, encoding)
	if err != nil {
		return Trailer{}, err
	}
	return m.trailer, nil
}

// TrailerInit returns the ISO-BMFF initialization segment for rep and its
// media type ("video/mp4" or "audio/mp4").
func (l *Lookup) TrailerInit(ctx context.Context, app, encoding, rep string) ([]byte, string, error) {
	if !appID(app) {
		return nil, "", ErrBadID
	}
	if !validEncoding(encoding) || !trailerRepIDRe.MatchString(rep) {
		return nil, "", ErrNotFound
	}
	m, err := l.trailerManifest(ctx, app, encoding)
	if err != nil {
		return nil, "", err
	}
	tr, ok := m.tracks[rep]
	if !ok {
		return nil, "", ErrNotFound
	}
	b, err := l.trailerBytes(ctx, "init "+m.baseDir+tr.initPath, m.baseDir+tr.initPath, isInitMP4)
	if err != nil {
		return nil, "", err
	}
	return b, tr.mime, nil
}

// TrailerSegment returns the 1-based media chunk seg for rep and its media
// type ("video/mp4" or "audio/mp4").
func (l *Lookup) TrailerSegment(ctx context.Context, app, encoding, rep string, seg int) ([]byte, string, error) {
	if !appID(app) {
		return nil, "", ErrBadID
	}
	if !validEncoding(encoding) || !trailerRepIDRe.MatchString(rep) || seg < 1 {
		return nil, "", ErrNotFound
	}
	m, err := l.trailerManifest(ctx, app, encoding)
	if err != nil {
		return nil, "", err
	}
	tr, ok := m.tracks[rep]
	if !ok || seg > tr.segments {
		return nil, "", ErrNotFound
	}
	rel := resolveMediaPath(encoding, tr.mediaTmpl, tr.startNumber+seg-1)
	if rel == "" {
		return nil, "", ErrUnavailable
	}
	b, err := l.trailerBytes(ctx, "seg "+m.baseDir+rel, m.baseDir+rel, isChunkMP4)
	if err != nil {
		return nil, "", err
	}
	return b, tr.mime, nil
}

func (l *Lookup) trailerManifest(ctx context.Context, app, encoding string) (parsedManifest, error) {
	if !appID(app) {
		return parsedManifest{}, ErrBadID
	}
	if !validEncoding(encoding) {
		return parsedManifest{}, ErrNotFound
	}
	it, err := l.item(ctx, app)
	if err != nil {
		return parsedManifest{}, err
	}
	cdnPath := it.trailers[encoding]
	if cdnPath == "" {
		return parsedManifest{}, ErrNotFound
	}
	v, err := l.trailers.Get(ctx, "mpd "+cdnPath, keepTrailer, func(ctx context.Context) (any, int, error) {
		return l.fetchManifest(ctx, encoding, cdnPath)
	})
	if err != nil {
		return parsedManifest{}, err
	}
	return v.(parsedManifest), nil
}

func (l *Lookup) fetchManifest(ctx context.Context, encoding, cdnPath string) (any, int, error) {
	resp, err := l.fetch.Ask(ctx, l.sites.video+cdnPath, "application/dash+xml, application/xml, */*")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, 0, ErrNotFound
	default:
		return nil, 0, fmt.Errorf("%w: Steam's trailer host answered %d", ErrUnavailable, resp.StatusCode)
	}
	body, err := l.fetch.ReadAll(resp.Body, maxManifest)
	if err != nil {
		return nil, 0, err
	}
	slash := strings.LastIndexByte(cdnPath, '/')
	if slash < 0 {
		return nil, 0, fmt.Errorf("%w: Steam's trailer path has no directory", ErrUnavailable)
	}
	m, err := parseMPD(encoding, cdnPath[:slash+1], body)
	if err != nil {
		return nil, 0, err
	}
	return m, len(body) + 512, nil
}

func (l *Lookup) trailerBytes(ctx context.Context, key, relPath string, sniff func([]byte) bool) ([]byte, error) {
	v, err := l.trailers.Get(ctx, key, keepTrailer, func(ctx context.Context) (any, int, error) {
		resp, err := l.fetch.Ask(ctx, l.sites.video+relPath, "*/*")
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound:
			return nil, 0, ErrNotFound
		default:
			return nil, 0, fmt.Errorf("%w: Steam's trailer host answered %d", ErrUnavailable, resp.StatusCode)
		}
		body, err := l.fetch.ReadAll(resp.Body, maxSegment)
		if err != nil {
			return nil, 0, err
		}
		if !sniff(body) {
			return nil, 0, fmt.Errorf("%w: Steam's trailer segment isn't ISO-BMFF", ErrUnavailable)
		}
		return body, len(body), nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

type mpdDoc struct {
	XMLName                   xml.Name    `xml:"MPD"`
	MediaPresentationDuration string      `xml:"mediaPresentationDuration,attr"`
	Periods                   []mpdPeriod `xml:"Period"`
}

type mpdPeriod struct {
	Duration       string          `xml:"duration,attr"`
	AdaptationSets []mpdAdaptation `xml:"AdaptationSet"`
}

type mpdAdaptation struct {
	ContentType     string              `xml:"contentType,attr"`
	MimeType        string              `xml:"mimeType,attr"`
	Codecs          string              `xml:"codecs,attr"`
	SegmentTemplate *mpdSegmentTemplate `xml:"SegmentTemplate"`
	Representations []mpdRepresentation `xml:"Representation"`
}

type mpdRepresentation struct {
	ID              string              `xml:"id,attr"`
	MimeType        string              `xml:"mimeType,attr"`
	Codecs          string              `xml:"codecs,attr"`
	Width           int                 `xml:"width,attr"`
	Height          int                 `xml:"height,attr"`
	Bandwidth       int                 `xml:"bandwidth,attr"`
	SegmentTemplate *mpdSegmentTemplate `xml:"SegmentTemplate"`
	SegmentBase     *struct{}           `xml:"SegmentBase"`
	SegmentList     *struct{}           `xml:"SegmentList"`
}

type mpdSegmentTemplate struct {
	Timescale       int64               `xml:"timescale,attr"`
	Duration        int64               `xml:"duration,attr"`
	StartNumber     int                 `xml:"startNumber,attr"`
	Initialization  string              `xml:"initialization,attr"`
	Media           string              `xml:"media,attr"`
	SegmentTimeline *mpdSegmentTimeline `xml:"SegmentTimeline"`
}

type mpdSegmentTimeline struct {
	S []struct {
		T int64 `xml:"t,attr"`
		D int64 `xml:"d,attr"`
		R int   `xml:"r,attr"`
	} `xml:"S"`
}

// checkMPDXML rejects DTDs/entities, BaseURL elements anywhere in the document,
// and non-MPD roots before unmarshaling.
func checkMPDXML(body []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = true
	dec.Entity = map[string]string{}
	sawRoot := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: Steam's trailer manifest isn't valid XML", ErrUnavailable)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: Steam's trailer manifest holds an XML directive", ErrUnavailable)
		case xml.StartElement:
			if !sawRoot {
				if t.Name.Local != "MPD" {
					return fmt.Errorf("%w: Steam's trailer manifest has root %q", ErrUnavailable, t.Name.Local)
				}
				sawRoot = true
			}
			if strings.EqualFold(t.Name.Local, "BaseURL") {
				return fmt.Errorf("%w: Steam's trailer manifest uses BaseURL", ErrUnavailable)
			}
		}
	}
	if !sawRoot {
		return fmt.Errorf("%w: Steam's trailer manifest is empty", ErrUnavailable)
	}
	return nil
}

func parseMPD(enc, baseDir string, body []byte) (parsedManifest, error) {
	if err := checkMPDXML(body); err != nil {
		return parsedManifest{}, err
	}
	var doc mpdDoc
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = true
	dec.Entity = map[string]string{}
	if err := dec.Decode(&doc); err != nil {
		return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest couldn't be read", ErrUnavailable)
	}
	if len(doc.Periods) == 0 {
		return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest has no Period", ErrUnavailable)
	}
	p := doc.Periods[0]
	dur := parseISO8601Duration(p.Duration)
	if dur <= 0 {
		dur = parseISO8601Duration(doc.MediaPresentationDuration)
	}
	if dur > maxTrailerDuration {
		return parsedManifest{}, fmt.Errorf("%w: Steam's trailer duration is out of range", ErrUnavailable)
	}

	var videoTracks []TrailerTrack
	var bestAudio *TrailerTrack
	var bestAudioTrack manifestTrack
	bestAudioBW := -1
	videoTrackMeta := map[string]manifestTrack{}
	seenIDs := map[string]bool{}
	parsedReps := 0

	for _, as := range p.AdaptationSets {
		if strings.HasPrefix(as.MimeType, "image/") || as.ContentType == "image" {
			continue
		}
		for _, rep := range as.Representations {
			mt := firstNonEmpty(rep.MimeType, as.MimeType)
			if strings.HasPrefix(mt, "image/") {
				continue
			}
			var isVideo bool
			switch {
			case mt == "video/mp4":
				if as.ContentType != "" && as.ContentType != "video" {
					return parsedManifest{}, fmt.Errorf("%w: mismatched video contentType", ErrUnavailable)
				}
				isVideo = true
			case mt == "audio/mp4":
				if as.ContentType != "" && as.ContentType != "audio" {
					return parsedManifest{}, fmt.Errorf("%w: mismatched audio contentType", ErrUnavailable)
				}
				isVideo = false
			case as.ContentType == "video" || as.ContentType == "audio" || strings.HasPrefix(mt, "video/") || strings.HasPrefix(mt, "audio/"):
				return parsedManifest{}, fmt.Errorf("%w: Steam's trailer uses unsupported media type %q", ErrUnavailable, mt)
			default:
				continue
			}
			parsedReps++
			if parsedReps > maxTrailerReps {
				return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest has too many representations", ErrUnavailable)
			}
			if !trailerRepIDRe.MatchString(rep.ID) || seenIDs[rep.ID] {
				return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest has an invalid representation ID", ErrUnavailable)
			}
			seenIDs[rep.ID] = true
			if rep.SegmentBase != nil || rep.SegmentList != nil {
				return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest uses unsupported segment addressing", ErrUnavailable)
			}
			tmpl := rep.SegmentTemplate
			if tmpl == nil {
				tmpl = as.SegmentTemplate
			}
			if tmpl == nil {
				return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest lacks SegmentTemplate", ErrUnavailable)
			}
			codecs := firstNonEmpty(rep.Codecs, as.Codecs)
			if !validCodecs(isVideo, enc, codecs) {
				return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest has unexpected codecs", ErrUnavailable)
			}
			initPath, mediaTmpl, err := checkSegmentTemplatePaths(enc, rep.ID, tmpl.Initialization, tmpl.Media)
			if err != nil {
				return parsedManifest{}, err
			}
			startNum, segCount, segDur, trackDur, err := checkSegmentTiming(tmpl, dur)
			if err != nil {
				return parsedManifest{}, err
			}
			if dur <= 0 && trackDur > 0 {
				dur = trackDur
			}
			track := TrailerTrack{
				ID:              rep.ID,
				Mime:            mt,
				Codecs:          codecs,
				Segments:        segCount,
				SegmentDuration: segDur,
			}
			meta := manifestTrack{
				mime:        mt,
				initPath:    initPath,
				mediaTmpl:   mediaTmpl,
				startNumber: startNum,
				segments:    segCount,
			}
			if isVideo {
				if rep.Width < 1 || rep.Width > 4096 || rep.Height < 1 || rep.Height > 4096 {
					return parsedManifest{}, fmt.Errorf("%w: Steam's trailer dimensions are out of range", ErrUnavailable)
				}
				track.Width = rep.Width
				track.Height = rep.Height
				videoTracks = append(videoTracks, track)
				videoTrackMeta[rep.ID] = meta
			} else if bestAudio == nil || rep.Bandwidth > bestAudioBW {
				tCopy := track
				bestAudio = &tCopy
				bestAudioTrack = meta
				bestAudioBW = rep.Bandwidth
			}
		}
	}

	if len(videoTracks) == 0 || dur <= 0 || dur > maxTrailerDuration {
		return parsedManifest{}, fmt.Errorf("%w: Steam's trailer manifest holds no playable video", ErrUnavailable)
	}
	slices.SortFunc(videoTracks, func(a, b TrailerTrack) int {
		if c := cmp.Compare(a.Height, b.Height); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Width, b.Width); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})

	tracks := videoTrackMeta
	if bestAudio != nil {
		tracks[bestAudio.ID] = bestAudioTrack
	}
	return parsedManifest{
		trailer: Trailer{
			Encoding: enc,
			Duration: dur,
			Video:    videoTracks,
			Audio:    bestAudio,
		},
		baseDir: baseDir,
		tracks:  tracks,
	}, nil
}

func checkSegmentTemplatePaths(enc, repID, initTmpl, mediaTmpl string) (string, string, error) {
	for _, s := range []string{initTmpl, mediaTmpl} {
		if s == "" || strings.ContainsAny(s, "?#\\ \t\r\n") || strings.Contains(s, "..") {
			return "", "", fmt.Errorf("%w: Steam's trailer template path is rejected", ErrUnavailable)
		}
	}
	prefix := "dash_" + enc + "/"
	initPath := strings.ReplaceAll(initTmpl, "$RepresentationID$", repID)
	if !strings.HasPrefix(initPath, prefix) || !trailerInitRelRe.MatchString(initPath) {
		return "", "", fmt.Errorf("%w: Steam's trailer init template is rejected", ErrUnavailable)
	}
	mediaSub := strings.ReplaceAll(mediaTmpl, "$RepresentationID$", repID)
	if strings.Count(mediaSub, "$Number%05d$") != 1 {
		return "", "", fmt.Errorf("%w: Steam's trailer media template is rejected", ErrUnavailable)
	}
	if resolveMediaPath(enc, mediaSub, 1) == "" {
		return "", "", fmt.Errorf("%w: Steam's trailer media path is rejected", ErrUnavailable)
	}
	return initPath, mediaSub, nil
}

func resolveMediaPath(enc, mediaTmpl string, num int) string {
	if num < 1 || num > 99999 {
		return ""
	}
	p := strings.Replace(mediaTmpl, "$Number%05d$", fmt.Sprintf("%05d", num), 1)
	if !strings.HasPrefix(p, "dash_"+enc+"/") || !trailerChunkRelRe.MatchString(p) {
		return ""
	}
	return p
}

func checkSegmentTiming(tmpl *mpdSegmentTemplate, manifestDur float64) (startNum, count int, segDur, trackDur float64, err error) {
	startNum = tmpl.StartNumber
	if startNum == 0 {
		startNum = 1
	}
	if startNum < 1 || startNum > maxTrailerSegments {
		return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer startNumber is out of range", ErrUnavailable)
	}
	ts := tmpl.Timescale
	if ts == 0 {
		ts = 1
	}
	if ts < 1 {
		return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer timescale is invalid", ErrUnavailable)
	}
	if tmpl.SegmentTimeline != nil && len(tmpl.SegmentTimeline.S) > 0 {
		var totalTicks int64
		for _, s := range tmpl.SegmentTimeline.S {
			if s.D <= 0 || s.R < 0 || s.R >= maxTrailerSegments {
				return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer timeline is invalid", ErrUnavailable)
			}
			count += s.R + 1
			if count > maxTrailerSegments {
				return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer has too many segments", ErrUnavailable)
			}
			totalTicks += int64(s.R+1) * s.D
		}
		segDur = float64(tmpl.SegmentTimeline.S[0].D) / float64(ts)
		trackDur = float64(totalTicks) / float64(ts)
		if segDur <= 0 || segDur > maxSegmentDuration || trackDur <= 0 || trackDur > maxTrailerDuration {
			return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer timeline duration is out of range", ErrUnavailable)
		}
		return startNum, count, segDur, trackDur, nil
	}
	if tmpl.Duration <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer segment duration is missing", ErrUnavailable)
	}
	segDur = float64(tmpl.Duration) / float64(ts)
	if segDur <= 0 || segDur > maxSegmentDuration || manifestDur <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer duration is invalid", ErrUnavailable)
	}
	count = max(1, int(math.Ceil(manifestDur/segDur-1e-6)))
	if count > maxTrailerSegments {
		return 0, 0, 0, 0, fmt.Errorf("%w: Steam's trailer has too many segments", ErrUnavailable)
	}
	return startNum, count, segDur, manifestDur, nil
}

func validCodecs(isVideo bool, enc, codecs string) bool {
	if !trailerCodecsRe.MatchString(codecs) {
		return false
	}
	if !isVideo {
		return strings.HasPrefix(codecs, "mp4a.")
	}
	switch enc {
	case "h264":
		return strings.HasPrefix(codecs, "avc1.") || strings.HasPrefix(codecs, "avc3.")
	case "av1":
		return strings.HasPrefix(codecs, "av01.")
	default:
		return false
	}
}

func parseISO8601Duration(s string) float64 {
	m := isoDurationRe.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "") {
		return 0
	}
	var sec float64
	if m[1] != "" {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		sec += v * 3600
	}
	if m[2] != "" {
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		sec += v * 60
	}
	if m[3] != "" {
		v, err := strconv.ParseFloat(m[3], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		sec += v
	}
	if math.IsNaN(sec) || math.IsInf(sec, 0) || sec <= 0 {
		return 0
	}
	return sec
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func validBoxHeader(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	sz := binary.BigEndian.Uint32(b[:4])
	return sz == 0 || sz == 1 || (sz >= 8 && int(sz) <= len(b))
}

func isInitMP4(b []byte) bool {
	return len(b) >= 12 && validBoxHeader(b) && string(b[4:8]) == "ftyp"
}

func isChunkMP4(b []byte) bool {
	return validBoxHeader(b) && (string(b[4:8]) == "styp" || string(b[4:8]) == "moof")
}
