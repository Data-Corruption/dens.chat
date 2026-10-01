// Dens's driver over FFmpeg's libraries: what Dens does with media, as four
// operations a WebAssembly module exports. It reads and writes only through
// the host's functions (host.h), so the module reaches nothing else.
//
//   dm_probe   describes a file: its container, streams and the names of its
//              metadata, never their values.
//   dm_strip   copies a file's video and audio into a new container, without
//              anything else it carried.
//   dm_still   turns an image a browser can't show into a JPEG or PNG.
//   dm_poster  makes a video's preview from its first frame.
//
// Each answers with JSON through host_result, and returns 0 or a negative
// FFmpeg error, which dm_error describes.

#include <math.h>
#include <stdint.h>
#include <string.h>

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/bprint.h>
#include <libavutil/display.h>
#include <libavutil/imgutils.h>
#include <libavutil/intreadwrite.h>
#include <libavutil/pixdesc.h>
#include <libswscale/swscale.h>

#include "host.h"

#define IO_BUFFER (64 * 1024)

static int log_level = AV_LOG_WARNING;

static void log_callback(void *avcl, int level, const char *fmt, va_list vl) {
    if (level > log_level) {
        return;
    }
    char line[1024];
    int prefix = 1;
    av_log_format_line2(avcl, level, fmt, vl, line, sizeof(line), &prefix);
    host_log(level, line, (int32_t)strlen(line));
}

DM_EXPORT(dm_init) void dm_init(int32_t level) {
    log_level = level;
    av_log_set_callback(log_callback);
}

// I/O. Each context keeps its own position in a host file, since a muxer
// moving its index to the front reads the output back while writing it.

typedef struct io_handle {
    int32_t file;
    int64_t pos;
} io_handle;

static int io_read(void *opaque, uint8_t *buf, int size) {
    io_handle *h = opaque;
    int32_t n = host_read(h->file, buf, size, h->pos);
    if (n == 0) {
        return AVERROR_EOF;
    }
    if (n < 0 || n > size) {
        return AVERROR(EIO);
    }
    h->pos += n;
    return n;
}

static int io_write(void *opaque, const uint8_t *buf, int size) {
    io_handle *h = opaque;
    if (host_write(h->file, buf, size, h->pos) != size) {
        return AVERROR(EIO);
    }
    h->pos += size;
    return size;
}

static int64_t io_seek(void *opaque, int64_t offset, int whence) {
    io_handle *h = opaque;
    int64_t size = host_size(h->file);
    switch (whence & ~AVSEEK_FORCE) {
    case AVSEEK_SIZE:
        return size < 0 ? AVERROR(EIO) : size;
    case SEEK_SET:
        break;
    case SEEK_CUR:
        offset += h->pos;
        break;
    case SEEK_END:
        if (size < 0) {
            return AVERROR(EIO);
        }
        offset += size;
        break;
    default:
        return AVERROR(EINVAL);
    }
    if (offset < 0) {
        return AVERROR(EINVAL);
    }
    h->pos = offset;
    return offset;
}

static AVIOContext *open_io(int32_t file, int writable) {
    io_handle *h = av_mallocz(sizeof(*h));
    uint8_t *buf = av_malloc(IO_BUFFER);
    AVIOContext *io = NULL;
    if (h && buf) {
        h->file = file;
        io = avio_alloc_context(buf, IO_BUFFER, writable, h, writable ? NULL : io_read,
                                writable ? io_write : NULL, io_seek);
    }
    if (!io) {
        av_free(h);
        av_free(buf);
    }
    return io;
}

static void close_io(AVIOContext **io) {
    if (*io) {
        av_freep(&(*io)->opaque);
        av_freep(&(*io)->buffer);
        avio_context_free(io);
    }
}

// A muxer moving its index to the front opens its output again to read it
// back; this hands it the output, by its own position.
static int io_open_output(struct AVFormatContext *s, AVIOContext **pb, const char *url, int flags,
                          AVDictionary **options) {
    (void)s, (void)url, (void)options;
    if (flags & AVIO_FLAG_WRITE) {
        return AVERROR(EPERM);
    }
    *pb = open_io(HOST_OUTPUT, 0);
    return *pb ? 0 : AVERROR(ENOMEM);
}

static int io_close_output(struct AVFormatContext *s, AVIOContext *pb) {
    (void)s;
    close_io(&pb);
    return 0;
}

static int open_input(AVFormatContext **ic, AVIOContext **io) {
    *io = open_io(HOST_INPUT, 0);
    *ic = avformat_alloc_context();
    if (!*io || !*ic) {
        avformat_free_context(*ic);
        *ic = NULL;
        return AVERROR(ENOMEM);
    }
    (*ic)->pb = *io;
    int ret = avformat_open_input(ic, NULL, NULL, NULL);
    if (ret < 0) {
        return ret;
    }
    // Without decoders some parameters stay unknown, which a copy doesn't need.
    avformat_find_stream_info(*ic, NULL);
    return 0;
}

static int finish(AVBPrint *b, int ret) {
    if (ret >= 0 && !av_bprint_is_complete(b)) {
        ret = AVERROR(ENOMEM);
    }
    if (ret >= 0) {
        host_result(b->str, (int32_t)b->len);
    }
    av_bprint_finalize(b, NULL);
    return ret;
}

static const AVPacketSideData *side_data(const AVPacketSideData *sd, int nb, enum AVPacketSideDataType type) {
    return av_packet_side_data_get(sd, nb, type);
}

// Turns. The ways a display matrix can ask for an image to be turned, as the
// ffmpeg command reads it: none, flips, and the four that swap width and
// height.

enum turn { TURN_NONE, TURN_HFLIP, TURN_VFLIP, TURN_180, TURN_TRANSPOSE, TURN_CLOCK, TURN_CCLOCK, TURN_CLOCK_FLIP };

static const char *const turn_names[] = {"none", "hflip", "vflip", "180", "transpose", "clock", "cclock", "clock_flip"};

static int turn_swaps(enum turn t) {
    return t >= TURN_TRANSPOSE;
}

// turn_for follows fftools/ffmpeg_filter.c, so an image comes out the way
// the ffmpeg command would show it. An angle that isn't a quarter turn is
// left alone.
static enum turn turn_for(const int32_t *m) {
    if (!m) {
        return TURN_NONE;
    }
    double theta = -round(av_display_rotation_get(m));
    theta -= 360 * floor(theta / 360 + 0.9 / 360);
    if (fabs(theta - 90) < 1.0) {
        return m[3] > 0 ? TURN_TRANSPOSE : TURN_CLOCK;
    }
    if (fabs(theta - 180) < 1.0) {
        int h = m[0] < 0, v = m[4] < 0;
        return h && v ? TURN_180 : h ? TURN_HFLIP : v ? TURN_VFLIP : TURN_NONE;
    }
    if (fabs(theta - 270) < 1.0) {
        return m[3] < 0 ? TURN_CLOCK_FLIP : TURN_CCLOCK;
    }
    if (fabs(theta) < 1.0 && m[4] < 0) {
        return TURN_VFLIP;
    }
    return TURN_NONE;
}

static const int32_t *display_matrix(const AVPacketSideData *sd, int nb) {
    const AVPacketSideData *m = side_data(sd, nb, AV_PKT_DATA_DISPLAYMATRIX);
    return m && m->size >= 9 * 4 ? (const int32_t *)m->data : NULL;
}

// stream_sar is a stream's sample aspect ratio, the container's first, as
// stripping copies it.
static AVRational stream_sar(const AVStream *st) {
    return st->sample_aspect_ratio.num ? st->sample_aspect_ratio : st->codecpar->sample_aspect_ratio;
}

// stretch gives the size a browser shows w × h pixels of sample aspect
// ratio sar at: wider for wide pixels, taller for tall ones.
static void stretch(int *w, int *h, AVRational sar) {
    if (sar.num <= 0 || sar.den <= 0 || sar.num == sar.den) {
        return;
    }
    if (sar.num > sar.den) {
        *w = (int)FFMIN(lround((double)*w * sar.num / sar.den), 1 << 16);
    } else {
        *h = (int)FFMIN(lround((double)*h * sar.den / sar.num), 1 << 16);
    }
}

// shown_size gives the size a video stream shows at before its turn:
// cropped as its container asks, then stretched by its sample aspect ratio.
static void shown_size(const AVStream *st, int *w, int *h) {
    const AVCodecParameters *par = st->codecpar;
    *w = par->width;
    *h = par->height;
    const AVPacketSideData *crop = side_data(par->coded_side_data, par->nb_coded_side_data, AV_PKT_DATA_FRAME_CROPPING);
    if (crop && crop->size >= 16) {
        uint64_t t = AV_RL32(crop->data), b = AV_RL32(crop->data + 4);
        uint64_t l = AV_RL32(crop->data + 8), r = AV_RL32(crop->data + 12);
        if (l + r < (uint64_t)*w && t + b < (uint64_t)*h) {
            *w -= (int)(l + r);
            *h -= (int)(t + b);
        }
    }
    stretch(w, h, stream_sar(st));
}

// Probing.

static void put_string(AVBPrint *b, const char *s) {
    av_bprintf(b, "\"");
    for (; *s; s++) {
        av_bprintf(b, *s == '"' || *s == '\\' ? "\\%c" : (uint8_t)*s < 0x20 ? "?" : "%c", *s);
    }
    av_bprintf(b, "\"");
}

static void put_keys(AVBPrint *b, const AVDictionary *m) {
    const AVDictionaryEntry *e = NULL;
    av_bprintf(b, "[");
    for (int i = 0; (e = av_dict_iterate(m, e)); i++) {
        if (i) {
            av_bprintf(b, ",");
        }
        put_string(b, e->key);
    }
    av_bprintf(b, "]");
}

static void put_side_data(AVBPrint *b, const AVCodecParameters *par) {
    av_bprintf(b, "[");
    for (int j = 0; j < par->nb_coded_side_data; j++) {
        const char *name = av_packet_side_data_name(par->coded_side_data[j].type);
        av_bprintf(b, "%s", j ? "," : "");
        put_string(b, name ? name : "?");
    }
    av_bprintf(b, "]");
}

// dm_probe describes the input: its container, its duration, its streams
// with the size a video shows at and whether the module can decode it for
// a poster, and the names of its metadata, never their values.
DM_EXPORT(dm_probe) int32_t dm_probe(void) {
    AVFormatContext *ic = NULL;
    AVIOContext *io = NULL;
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);
    int ret = open_input(&ic, &io);
    if (ret < 0) {
        goto end;
    }
    av_bprintf(&b, "{\"format\":");
    put_string(&b, ic->iformat->name);
    av_bprintf(&b, ",\"duration_ms\":%" PRId64 ",\"chapters\":%u,\"metadata\":",
               ic->duration > 0 ? ic->duration / 1000 : 0, ic->nb_chapters);
    put_keys(&b, ic->metadata);
    av_bprintf(&b, ",\"stream_groups\":%u,\"streams\":[", ic->nb_stream_groups);
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        const AVStream *st = ic->streams[i];
        const AVCodecParameters *par = st->codecpar;
        const char *type = av_get_media_type_string(par->codec_type);
        av_bprintf(&b, "%s{\"type\":", i ? "," : "");
        put_string(&b, type ? type : "unknown");
        av_bprintf(&b, ",\"codec\":");
        put_string(&b, avcodec_get_name(par->codec_id));
        if (par->codec_type == AVMEDIA_TYPE_VIDEO) {
            enum turn t = turn_for(display_matrix(par->coded_side_data, par->nb_coded_side_data));
            int w, h;
            shown_size(st, &w, &h);
            av_bprintf(&b, ",\"width\":%d,\"height\":%d,\"turn\":\"%s\",\"attached_pic\":%s,\"decoder\":%s",
                       turn_swaps(t) ? h : w, turn_swaps(t) ? w : h, turn_names[t],
                       st->disposition & AV_DISPOSITION_ATTACHED_PIC ? "true" : "false",
                       avcodec_find_decoder(par->codec_id) ? "true" : "false");
        }
        av_bprintf(&b, ",\"side_data\":");
        put_side_data(&b, par);
        av_bprintf(&b, ",\"metadata\":");
        put_keys(&b, st->metadata);
        av_bprintf(&b, "}");
    }
    av_bprintf(&b, "]}");

end:
    avformat_close_input(&ic);
    close_io(&io);
    return finish(&b, ret);
}

// Stripping.

// copied says whether a stream goes into the stripped copy: video and audio,
// and nothing else. Subtitles go because some cameras write coordinates as
// subtitles; cover art, data and timed-metadata tracks go with the metadata.
static int copied(const AVStream *st) {
    enum AVMediaType type = st->codecpar->codec_type;
    return (type == AVMEDIA_TYPE_VIDEO || type == AVMEDIA_TYPE_AUDIO) && !(st->disposition & AV_DISPOSITION_ATTACHED_PIC);
}

// fits says whether a muxer takes every copied stream. Some muxers can't say
// what they take, as Ogg with Opus; only a refusal counts against them, as
// with the ffmpeg command, which just tries.
static int fits(const AVFormatContext *ic, const char *muxer) {
    const AVOutputFormat *of = av_guess_format(muxer, NULL, NULL);
    if (!of) {
        return 0;
    }
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        if (copied(ic->streams[i]) && avformat_query_codec(of, ic->streams[i]->codecpar->codec_id, FF_COMPLIANCE_NORMAL) == 0) {
            return 0;
        }
    }
    return 1;
}

// choose_muxer picks the container a stripped copy is written in: MP4 for
// what came as MP4 or MOV, since Chrome won't play a MOV; WebM for what
// browsers play as WebM; and otherwise the input's own kind. It says the
// type the local service serves the copy as.
static const char *choose_muxer(const AVFormatContext *ic, const char **mime) {
    int video = 0;
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        video |= copied(ic->streams[i]) && ic->streams[i]->codecpar->codec_type == AVMEDIA_TYPE_VIDEO;
    }
    const char *in = ic->iformat->name;
    if (!strcmp(in, "mov,mp4,m4a,3gp,3g2,mj2") || !strcmp(in, "aac")) {
        if (!video && fits(ic, "ipod")) {
            *mime = "audio/mp4";
            return "ipod";
        }
        *mime = video ? "video/mp4" : "audio/mp4";
        return "mp4";
    }
    if (!strcmp(in, "matroska,webm")) {
        if (fits(ic, "webm")) {
            *mime = video ? "video/webm" : "audio/webm";
            return "webm";
        }
        if (fits(ic, "mp4")) {
            *mime = video ? "video/mp4" : "audio/mp4";
            return "mp4";
        }
        *mime = video ? "video/x-matroska" : "audio/x-matroska";
        return "matroska";
    }
    if (!strcmp(in, "mp3")) {
        *mime = "audio/mpeg";
        return "mp3";
    }
    if (!strcmp(in, "flac")) {
        *mime = "audio/flac";
        return "flac";
    }
    if (!strcmp(in, "ogg")) {
        *mime = "audio/ogg";
        return "ogg";
    }
    if (!strcmp(in, "wav")) {
        *mime = "audio/wav";
        return "wav";
    }
    return NULL;
}

// keep_side_data says whether a stream's side data goes into the stripped
// copy: what playback needs, and the ICC profile, which Dens keeps in images
// too. Anything else the container attached, such as a HEIF image's EXIF,
// stays behind.
static int keep_side_data(enum AVPacketSideDataType type) {
    switch (type) {
    case AV_PKT_DATA_ICC_PROFILE:
    case AV_PKT_DATA_DISPLAYMATRIX:
    case AV_PKT_DATA_STEREO3D:
    case AV_PKT_DATA_SPHERICAL:
    case AV_PKT_DATA_FRAME_CROPPING:
    case AV_PKT_DATA_DOVI_CONF:
    case AV_PKT_DATA_HEVC_CONF:
    case AV_PKT_DATA_CONTENT_LIGHT_LEVEL:
    case AV_PKT_DATA_MASTERING_DISPLAY_METADATA:
    case AV_PKT_DATA_AMBIENT_VIEWING_ENVIRONMENT:
    case AV_PKT_DATA_DYNAMIC_HDR10_PLUS:
        return 1;
    default:
        return 0;
    }
}

// drop_side_data removes what keep_side_data refuses, and counts it. It walks
// backward: removing an entry moves the last one into its place, and every
// entry past this one is already one to keep.
static int drop_side_data(AVCodecParameters *par) {
    int dropped = 0;
    for (int i = par->nb_coded_side_data - 1; i >= 0; i--) {
        enum AVPacketSideDataType type = par->coded_side_data[i].type;
        if (!keep_side_data(type)) {
            av_packet_side_data_remove(par->coded_side_data, &par->nb_coded_side_data, type);
            dropped++;
        }
    }
    return dropped;
}

// The ffmpeg command's rule for a stream copy: keep the input's codec tag when
// the output format accepts it, so HEVC stays hvc1 for Apple's players.
static uint32_t codec_tag(const AVOutputFormat *of, const AVCodecParameters *par) {
    unsigned int tmp;
    if (!of->codec_tag || av_codec_get_id(of->codec_tag, par->codec_tag) == par->codec_id ||
        !av_codec_get_tag2(of->codec_tag, par->codec_id, &tmp)) {
        return par->codec_tag;
    }
    return 0;
}

// The deepest frame reordering H.264 and HEVC allow.
#define MAX_REORDER 16

// reorder derives a video's decode times from its presentation times, as
// FFmpeg's muxer once did: each packet's decode time is the earliest of the
// last MAX_REORDER + 1 presentation times, which only grows while the
// stream reorders no deeper than that. The window starts filled with times
// a frame apart before the first, so the first decode times grow too.
// Matroska stores presentation times only, and without decoders FFmpeg
// can't tell how far a stream reorders, so the decode times it infers can
// run backwards.
typedef struct reorder {
    int64_t pts[MAX_REORDER + 1];
    int started;
} reorder;

static int64_t reorder_dts(reorder *r, int64_t pts, int64_t duration) {
    if (!r->started) {
        duration = FFMAX(duration, 1);
        for (int i = 1; i <= MAX_REORDER; i++) {
            r->pts[i] = pts + (i - MAX_REORDER - 1) * duration;
        }
        r->started = 1;
    }
    r->pts[0] = pts;
    for (int i = 0; i < MAX_REORDER && r->pts[i] > r->pts[i + 1]; i++) {
        FFSWAP(int64_t, r->pts[i], r->pts[i + 1]);
    }
    return r->pts[0];
}

// SEI user data. H.264 and HEVC carry supplemental messages inside the
// stream, and user_data_unregistered (payload 5) is a vendor's own bytes:
// iPhones write 19 in every frame. Dens can't tell what's in them, so a
// supplemental unit that carries any goes, whole; one that can't be read
// goes too.

typedef struct sei_filter {
    int hevc;
    int length_size; // the size of each unit's length, 1 to 4; 0 leaves the stream alone
} sei_filter;

static sei_filter sei_filter_for(const AVCodecParameters *par) {
    sei_filter f = {0};
    const uint8_t *x = par->extradata;
    if (par->codec_id == AV_CODEC_ID_H264 && par->extradata_size >= 7 && x[0] == 1) {
        f.length_size = (x[4] & 3) + 1;
    } else if (par->codec_id == AV_CODEC_ID_HEVC && par->extradata_size >= 23 && x[0] == 1) {
        f.length_size = (x[21] & 3) + 1;
        f.hevc = 1;
    }
    return f;
}

// sei_has_user_data says whether an SEI unit's payloads include user data,
// or can't be read. unit points past the unit's header.
static int sei_has_user_data(const uint8_t *unit, int size) {
    // The payload headers are read through emulation prevention: 00 00 03
    // stands for 00 00.
    uint8_t rbsp[512];
    int n = 0, zeros = 0;
    for (int i = 0; i < size && n < (int)sizeof(rbsp); i++) {
        if (zeros >= 2 && unit[i] == 3) {
            zeros = 0;
            continue;
        }
        zeros = unit[i] == 0 ? zeros + 1 : 0;
        rbsp[n++] = unit[i];
    }
    int i = 0;
    while (i < n) {
        if (rbsp[i] == 0x80 && i == n - 1) {
            return 0; // the trailing bits: every payload was read
        }
        int type = 0, len = 0;
        while (i < n && rbsp[i] == 0xFF) {
            type += 255;
            i++;
        }
        if (i >= n) {
            return 1;
        }
        type += rbsp[i++];
        while (i < n && rbsp[i] == 0xFF) {
            len += 255;
            i++;
        }
        if (i >= n) {
            return 1;
        }
        len += rbsp[i++];
        if (type == 5) {
            return 1;
        }
        i += len;
    }
    // Ran past what was read: a unit longer than the window, or malformed.
    return i != n;
}

// filter_sei drops the SEI units of a packet that carry user data, and
// counts them. A packet whose units don't add up is left alone, for the
// muxer to refuse or a player to skip.
static int filter_sei(const sei_filter *f, AVPacket *pkt, int *dropped) {
    if (!f->length_size) {
        return 0;
    }
    int ls = f->length_size, drop = 0;
    for (int pos = 0; pos + ls <= pkt->size;) {
        uint32_t len = 0;
        for (int k = 0; k < ls; k++) {
            len = len << 8 | pkt->data[pos + k];
        }
        if (len == 0 || len > (uint32_t)(pkt->size - pos - ls)) {
            return 0;
        }
        const uint8_t *unit = pkt->data + pos + ls;
        int header = f->hevc ? 2 : 1;
        int type = f->hevc ? (unit[0] >> 1) & 0x3f : unit[0] & 0x1f;
        int sei = f->hevc ? type == 39 || type == 40 : type == 6;
        if (sei && (len <= (uint32_t)header || sei_has_user_data(unit + header, (int)len - header))) {
            drop = 1;
        }
        pos += ls + (int)len;
    }
    if (!drop) {
        return 0;
    }
    AVPacket *out = av_packet_alloc();
    int ret = out ? av_new_packet(out, pkt->size) : AVERROR(ENOMEM);
    if (ret < 0) {
        av_packet_free(&out);
        return ret;
    }
    int n = 0;
    for (int pos = 0; pos + ls <= pkt->size;) {
        uint32_t len = 0;
        for (int k = 0; k < ls; k++) {
            len = len << 8 | pkt->data[pos + k];
        }
        const uint8_t *unit = pkt->data + pos + ls;
        int header = f->hevc ? 2 : 1;
        int type = f->hevc ? (unit[0] >> 1) & 0x3f : unit[0] & 0x1f;
        int sei = f->hevc ? type == 39 || type == 40 : type == 6;
        if (sei && (len <= (uint32_t)header || sei_has_user_data(unit + header, (int)len - header))) {
            (*dropped)++;
        } else {
            memcpy(out->data + n, pkt->data + pos, ls + len);
            n += ls + (int)len;
        }
        pos += ls + (int)len;
    }
    if ((ret = av_packet_copy_props(out, pkt)) < 0) {
        av_packet_free(&out);
        return ret;
    }
    av_shrink_packet(out, n);
    av_packet_unref(pkt);
    av_packet_move_ref(pkt, out);
    av_packet_free(&out);
    return 0;
}

// dm_strip copies the input's video and audio into a new container of the
// named format, or the one choose_muxer picks when muxer is empty, with none
// of the input's metadata, chapters or other streams. What playback needs
// travels with the codec parameters, rotation included. An MP4's index goes
// at the front, so a player starts before the whole file arrives.
DM_EXPORT(dm_strip) int32_t dm_strip(const char *muxer) {
    AVFormatContext *ic = NULL, *oc = NULL;
    AVIOContext *in = NULL, *out = NULL;
    AVPacket *pkt = av_packet_alloc();
    AVDictionary *opts = NULL;
    int *map = NULL;
    reorder *reorders = NULL;
    sei_filter *seis = NULL;
    int kept = 0, dropped = 0, side_dropped = 0, sei_dropped = 0;
    unsigned nb_streams = 0;
    const char *mime = "";
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);

    int ret = pkt ? open_input(&ic, &in) : AVERROR(ENOMEM);
    if (ret < 0) {
        goto end;
    }
    if (!muxer || !*muxer) {
        if (!(muxer = choose_muxer(ic, &mime))) {
            ret = AVERROR_PATCHWELCOME;
            goto end;
        }
    }
    ret = avformat_alloc_output_context2(&oc, NULL, muxer, NULL);
    if (ret < 0) {
        goto end;
    }
    out = open_io(HOST_OUTPUT, 1);
    // Some formats add streams as they're read; those aren't copied.
    nb_streams = ic->nb_streams;
    map = av_calloc(nb_streams, sizeof(*map));
    reorders = av_calloc(nb_streams, sizeof(*reorders));
    seis = av_calloc(nb_streams, sizeof(*seis));
    if (!out || !map || !reorders || !seis) {
        ret = AVERROR(ENOMEM);
        goto end;
    }
    oc->pb = out;
    oc->io_open = io_open_output;
    oc->io_close2 = io_close_output;
    // No "encoder" tag naming FFmpeg's version.
    oc->flags |= AVFMT_FLAG_BITEXACT;
    int pts_only = !strcmp(ic->iformat->name, "matroska,webm");

    for (unsigned i = 0; i < nb_streams; i++) {
        AVStream *ist = ic->streams[i];
        map[i] = -1;
        if (!copied(ist) || avformat_query_codec(oc->oformat, ist->codecpar->codec_id, FF_COMPLIANCE_NORMAL) == 0) {
            dropped++;
            continue;
        }
        AVStream *ost = avformat_new_stream(oc, NULL);
        if (!ost) {
            ret = AVERROR(ENOMEM);
            goto end;
        }
        ret = avcodec_parameters_copy(ost->codecpar, ist->codecpar);
        if (ret < 0) {
            goto end;
        }
        side_dropped += drop_side_data(ost->codecpar);
        ost->codecpar->codec_tag = codec_tag(oc->oformat, ist->codecpar);
        ost->time_base = ist->time_base;
        ost->avg_frame_rate = ist->avg_frame_rate;
        // As the ffmpeg command copies: the container's aspect ratio when it
        // states one, for the stream and the codec alike, which muxers check.
        AVRational sar = ist->sample_aspect_ratio.num ? ist->sample_aspect_ratio : ist->codecpar->sample_aspect_ratio;
        ost->sample_aspect_ratio = ost->codecpar->sample_aspect_ratio = sar;
        ost->disposition = ist->disposition & AV_DISPOSITION_DEFAULT;
        seis[i] = sei_filter_for(ist->codecpar);
        map[i] = ost->index;
        kept++;
    }
    if (!kept) {
        ret = AVERROR_STREAM_NOT_FOUND;
        goto end;
    }
    if (!strcmp(muxer, "mp4") || !strcmp(muxer, "mov") || !strcmp(muxer, "ipod")) {
        av_dict_set(&opts, "movflags", "+faststart", 0);
    }
    ret = avformat_write_header(oc, &opts);
    if (ret < 0) {
        goto end;
    }
    while ((ret = av_read_frame(ic, pkt)) >= 0) {
        int o = (unsigned)pkt->stream_index < nb_streams ? map[pkt->stream_index] : -1;
        if (o < 0) {
            av_packet_unref(pkt);
            continue;
        }
        if ((ret = filter_sei(&seis[pkt->stream_index], pkt, &sei_dropped)) < 0) {
            goto end;
        }
        if (pts_only && pkt->pts != AV_NOPTS_VALUE &&
            ic->streams[pkt->stream_index]->codecpar->codec_type == AVMEDIA_TYPE_VIDEO) {
            pkt->dts = reorder_dts(&reorders[pkt->stream_index], pkt->pts, pkt->duration);
        }
        av_packet_rescale_ts(pkt, ic->streams[pkt->stream_index]->time_base, oc->streams[o]->time_base);
        pkt->stream_index = o;
        pkt->pos = -1;
        ret = av_interleaved_write_frame(oc, pkt);
        if (ret < 0) {
            goto end;
        }
    }
    if (ret == AVERROR_EOF) {
        ret = av_write_trailer(oc);
    }
    if (ret >= 0) {
        avio_flush(out);
        av_bprintf(&b, "{\"muxer\":");
        put_string(&b, muxer);
        av_bprintf(&b, ",\"mime\":");
        put_string(&b, mime);
        av_bprintf(&b, ",\"kept\":%d,\"dropped\":%d,\"side_data_dropped\":%d,\"sei_dropped\":%d,\"bytes\":%" PRId64 "}",
                   kept, dropped, side_dropped, sei_dropped, host_size(HOST_OUTPUT));
    }

end:
    av_dict_free(&opts);
    av_packet_free(&pkt);
    av_free(map);
    av_free(reorders);
    av_free(seis);
    avformat_close_input(&ic);
    close_io(&in);
    avformat_free_context(oc);
    close_io(&out);
    return finish(&b, ret);
}

// Stills and posters.

// turn_plane copies a plane of sw by sh elements of bpp bytes, turned.
static void turn_plane(const uint8_t *src, int ss, int sw, int sh, uint8_t *dst, int ds, int bpp, enum turn t) {
    int dw = turn_swaps(t) ? sh : sw, dh = turn_swaps(t) ? sw : sh;
    for (int y = 0; y < dh; y++) {
        uint8_t *row = dst + (ptrdiff_t)y * ds;
        for (int x = 0; x < dw; x++) {
            int sx, sy;
            switch (t) {
            case TURN_HFLIP: sx = sw - 1 - x; sy = y; break;
            case TURN_VFLIP: sx = x; sy = sh - 1 - y; break;
            case TURN_180: sx = sw - 1 - x; sy = sh - 1 - y; break;
            case TURN_TRANSPOSE: sx = y; sy = x; break;
            case TURN_CLOCK: sx = y; sy = sh - 1 - x; break;
            case TURN_CCLOCK: sx = sw - 1 - y; sy = x; break;
            case TURN_CLOCK_FLIP: sx = sw - 1 - y; sy = sh - 1 - x; break;
            default: sx = x; sy = y; break;
            }
            memcpy(row + (ptrdiff_t)x * bpp, src + (ptrdiff_t)sy * ss + (ptrdiff_t)sx * bpp, bpp);
        }
    }
}

static AVCodecContext *open_decoder(const AVCodecParameters *par) {
    const AVCodec *codec = avcodec_find_decoder(par->codec_id);
    if (!codec) {
        return NULL;
    }
    AVCodecContext *dec = avcodec_alloc_context3(codec);
    if (!dec) {
        return NULL;
    }
    dec->thread_count = 1;
    dec->flags |= AV_CODEC_FLAG_COPY_OPAQUE;
    if (avcodec_parameters_to_context(dec, par) < 0 || avcodec_open2(dec, codec, NULL) < 0) {
        avcodec_free_context(&dec);
    }
    return dec;
}

// crop_as_stored crops a decoded frame as its container asks, as the ffmpeg
// command does: phone videos store a cropping rectangle beside the stream.
static int crop_as_stored(AVFrame *frame, const AVPacketSideData *sd, int nb) {
    const AVPacketSideData *crop = side_data(sd, nb, AV_PKT_DATA_FRAME_CROPPING);
    if (!crop || crop->size < 16) {
        return 0;
    }
    frame->crop_top = AV_RL32(crop->data);
    frame->crop_bottom = AV_RL32(crop->data + 4);
    frame->crop_left = AV_RL32(crop->data + 8);
    frame->crop_right = AV_RL32(crop->data + 12);
    return av_frame_apply_cropping(frame, AV_FRAME_CROP_UNALIGNED);
}

// encode_image scales a frame to the size it shows at, stretched by sar, and
// to fit max_side (0 keeps that size), turns it, and encodes it as a JPEG, or
// a PNG when it has transparency, carrying only the ICC profile given. The encoders get a fresh frame, so nothing else the
// source carried, EXIF included, reaches them. It empties src once it's
// scaled, and holds at most two full images at a time: a 48-megapixel photo
// is 73 MB in each.
static int encode_image(AVFrame *src, AVRational sar, const int32_t *matrix, int max_side, int quality,
                        const uint8_t *icc, int icc_size, AVBPrint *b) {
    const AVPixFmtDescriptor *desc = av_pix_fmt_desc_get(src->format);
    int alpha = desc && (desc->flags & AV_PIX_FMT_FLAG_ALPHA);
    enum turn t = turn_for(matrix);
    int sw = src->width, sh = src->height, dw = sw, dh = sh;
    stretch(&dw, &dh, sar);
    int ow = turn_swaps(t) ? dh : dw, oh = turn_swaps(t) ? dw : dh;
    if (max_side > 0 && FFMAX(ow, oh) > max_side) {
        double k = (double)max_side / FFMAX(ow, oh);
        ow = FFMAX(1, (int)lround(ow * k));
        oh = FFMAX(1, (int)lround(oh * k));
    }
    int pw = turn_swaps(t) ? oh : ow, ph = turn_swaps(t) ? ow : oh;
    enum AVPixelFormat fmt = alpha ? AV_PIX_FMT_RGBA : AV_PIX_FMT_YUVJ420P;

    AVFrame *scaled = av_frame_alloc(), *out = av_frame_alloc();
    AVPacket *pkt = av_packet_alloc();
    AVCodecContext *enc = NULL;
    struct SwsContext *sws = NULL;
    int ret = scaled && out && pkt ? 0 : AVERROR(ENOMEM);
    if (ret < 0) {
        goto end;
    }
    // The profile may live on src, which goes once it's scaled.
    if (icc_size > 0) {
        AVFrameSideData *sd = av_frame_new_side_data(out, AV_FRAME_DATA_ICC_PROFILE, icc_size);
        if (!sd) {
            ret = AVERROR(ENOMEM);
            goto end;
        }
        memcpy(sd->data, icc, icc_size);
    }
    // Without a turn, the scaled image is the output.
    AVFrame *target = t == TURN_NONE ? out : scaled;
    target->format = fmt;
    target->width = pw;
    target->height = ph;
    if ((ret = av_frame_get_buffer(target, 0)) < 0) {
        goto end;
    }
    out->color_primaries = src->color_primaries;
    out->color_trc = src->color_trc;
    sws = sws_getContext(sw, sh, src->format, pw, ph, fmt, SWS_BICUBIC, NULL, NULL, NULL);
    if (!sws) {
        ret = AVERROR(EINVAL);
        goto end;
    }
    sws_setColorspaceDetails(sws,
                             sws_getCoefficients(src->colorspace == AVCOL_SPC_UNSPECIFIED ? SWS_CS_DEFAULT : src->colorspace),
                             src->color_range == AVCOL_RANGE_JPEG, sws_getCoefficients(SWS_CS_DEFAULT), 1, 0, 1 << 16,
                             1 << 16);
    if ((ret = sws_scale(sws, (const uint8_t *const *)src->data, src->linesize, 0, sh, target->data,
                         target->linesize)) < 0) {
        goto end;
    }
    av_frame_unref(src);
    if (t != TURN_NONE) {
        out->format = fmt;
        out->width = ow;
        out->height = oh;
        if ((ret = av_frame_get_buffer(out, 0)) < 0) {
            goto end;
        }
        if (alpha) {
            turn_plane(scaled->data[0], scaled->linesize[0], pw, ph, out->data[0], out->linesize[0], 4, t);
        } else {
            int cw = AV_CEIL_RSHIFT(pw, 1), ch = AV_CEIL_RSHIFT(ph, 1);
            turn_plane(scaled->data[0], scaled->linesize[0], pw, ph, out->data[0], out->linesize[0], 1, t);
            for (int p = 1; p < 3; p++) {
                turn_plane(scaled->data[p], scaled->linesize[p], cw, ch, out->data[p], out->linesize[p], 1, t);
            }
        }
        av_frame_unref(scaled);
    }
    out->color_range = AVCOL_RANGE_JPEG;

    const AVCodec *codec = avcodec_find_encoder(alpha ? AV_CODEC_ID_PNG : AV_CODEC_ID_MJPEG);
    if (!codec || !(enc = avcodec_alloc_context3(codec))) {
        ret = AVERROR_ENCODER_NOT_FOUND;
        goto end;
    }
    enc->width = ow;
    enc->height = oh;
    enc->pix_fmt = fmt;
    enc->time_base = (AVRational){1, 1};
    enc->color_range = AVCOL_RANGE_JPEG;
    // No comment naming the encoder.
    enc->flags |= AV_CODEC_FLAG_BITEXACT;
    if (!alpha) {
        enc->flags |= AV_CODEC_FLAG_QSCALE;
        enc->global_quality = out->quality = FF_QP2LAMBDA * quality;
    }
    if ((ret = avcodec_open2(enc, codec, NULL)) < 0 || (ret = avcodec_send_frame(enc, out)) < 0 ||
        (ret = avcodec_send_frame(enc, NULL)) < 0 || (ret = avcodec_receive_packet(enc, pkt)) < 0) {
        goto end;
    }
    if (host_write(HOST_OUTPUT, pkt->data, pkt->size, 0) != pkt->size) {
        ret = AVERROR(EIO);
        goto end;
    }
    av_bprintf(b, "\"format\":\"%s\",\"width\":%d,\"height\":%d,\"turn\":\"%s\",\"icc_bytes\":%d,\"bytes\":%d",
               alpha ? "png" : "jpeg", ow, oh, turn_names[t], icc_size, pkt->size);

end:
    sws_freeContext(sws);
    avcodec_free_context(&enc);
    av_packet_free(&pkt);
    av_frame_free(&scaled);
    av_frame_free(&out);
    return ret;
}

// picture_stream returns the first video stream that isn't cover art, or a
// negative error: cover art is metadata, never a file's picture.
static int picture_stream(const AVFormatContext *ic) {
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        const AVStream *st = ic->streams[i];
        if (st->codecpar->codec_type == AVMEDIA_TYPE_VIDEO && !(st->disposition & AV_DISPOSITION_ATTACHED_PIC)) {
            return (int)i;
        }
    }
    return AVERROR_STREAM_NOT_FOUND;
}

// decode_first returns the first frame decoded from stream st.
static int decode_first(AVFormatContext *ic, int st, AVFrame *frame) {
    AVCodecContext *dec = open_decoder(ic->streams[st]->codecpar);
    AVPacket *pkt = av_packet_alloc();
    int ret = !dec ? AVERROR_DECODER_NOT_FOUND : !pkt ? AVERROR(ENOMEM) : AVERROR(EAGAIN);
    while (ret == AVERROR(EAGAIN)) {
        int r = av_read_frame(ic, pkt);
        if (r < 0) {
            avcodec_send_packet(dec, NULL);
        } else if (pkt->stream_index != st) {
            av_packet_unref(pkt);
            continue;
        } else {
            avcodec_send_packet(dec, pkt);
            av_packet_unref(pkt);
        }
        ret = avcodec_receive_frame(dec, frame);
        if (r < 0 && ret == AVERROR(EAGAIN)) {
            ret = AVERROR_EOF;
        }
    }
    av_packet_free(&pkt);
    avcodec_free_context(&dec);
    return ret;
}

// decode_grid decodes a tile grid's tiles onto one canvas, cropped to the
// image the grid presents. Tiles outside the canvas are clipped, and a tile
// that never arrives leaves black.
static int decode_grid(AVFormatContext *ic, const AVStreamGroup *g, AVFrame *canvas, int *placed) {
    const AVStreamGroupTileGrid *grid = g->params.tile_grid;
    int *tile_of = av_malloc_array(ic->nb_streams, sizeof(*tile_of));
    AVCodecContext *dec = g->nb_streams ? open_decoder(g->streams[0]->codecpar) : NULL;
    AVPacket *pkt = av_packet_alloc();
    AVFrame *tile = av_frame_alloc();
    int ret = !dec ? AVERROR_DECODER_NOT_FOUND : tile_of && pkt && tile ? 0 : AVERROR(ENOMEM);
    if (ret < 0) {
        goto end;
    }
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        tile_of[i] = -1;
    }
    for (unsigned i = 0; i < grid->nb_tiles; i++) {
        if (grid->offsets[i].idx < g->nb_streams) {
            tile_of[g->streams[grid->offsets[i].idx]->index] = (int)i;
        }
    }
    *placed = 0;
    for (int eof = 0; !eof;) {
        int r = av_read_frame(ic, pkt);
        if (r < 0) {
            eof = 1;
            avcodec_send_packet(dec, NULL);
        } else if ((unsigned)pkt->stream_index >= ic->nb_streams || tile_of[pkt->stream_index] < 0) {
            av_packet_unref(pkt);
            continue;
        } else {
            pkt->opaque = (void *)(intptr_t)(tile_of[pkt->stream_index] + 1);
            avcodec_send_packet(dec, pkt);
            av_packet_unref(pkt);
        }
        while (avcodec_receive_frame(dec, tile) >= 0) {
            int i = (int)(intptr_t)tile->opaque - 1;
            if (!canvas->data[0]) {
                canvas->format = tile->format;
                canvas->width = grid->coded_width;
                canvas->height = grid->coded_height;
                canvas->color_range = tile->color_range;
                canvas->color_primaries = tile->color_primaries;
                canvas->color_trc = tile->color_trc;
                canvas->colorspace = tile->colorspace;
                if ((ret = av_frame_get_buffer(canvas, 0)) < 0) {
                    goto end;
                }
                ptrdiff_t ls[4] = {canvas->linesize[0], canvas->linesize[1], canvas->linesize[2], canvas->linesize[3]};
                av_image_fill_black(canvas->data, ls, canvas->format, canvas->color_range, canvas->width,
                                    canvas->height);
            }
            const AVPixFmtDescriptor *d = av_pix_fmt_desc_get(tile->format);
            if (i >= 0 && (unsigned)i < grid->nb_tiles && tile->format == canvas->format && d) {
                int x = grid->offsets[i].horizontal, y = grid->offsets[i].vertical;
                int w = FFMIN(tile->width, canvas->width - x), h = FFMIN(tile->height, canvas->height - y);
                if (x >= 0 && y >= 0 && w > 0 && h > 0) {
                    for (int p = 0; p < 4 && tile->data[p]; p++) {
                        int cy = p == 1 || p == 2 ? d->log2_chroma_h : 0;
                        int bytes = av_image_get_linesize(tile->format, w, p);
                        av_image_copy_plane(canvas->data[p] + (ptrdiff_t)(y >> cy) * canvas->linesize[p] +
                                                av_image_get_linesize(canvas->format, x, p),
                                            canvas->linesize[p], tile->data[p], tile->linesize[p], bytes,
                                            AV_CEIL_RSHIFT(h, cy));
                    }
                    (*placed)++;
                }
            }
            av_frame_unref(tile);
        }
    }
    if (!canvas->data[0]) {
        ret = AVERROR_INVALIDDATA;
        goto end;
    }
    canvas->crop_left = grid->horizontal_offset;
    canvas->crop_top = grid->vertical_offset;
    canvas->crop_right = grid->coded_width - grid->width - grid->horizontal_offset;
    canvas->crop_bottom = grid->coded_height - grid->height - grid->vertical_offset;
    ret = av_frame_apply_cropping(canvas, AV_FRAME_CROP_UNALIGNED);

end:
    av_free(tile_of);
    av_frame_free(&tile);
    av_packet_free(&pkt);
    avcodec_free_context(&dec);
    return ret;
}

// dm_still converts an image the browser can't show or Dens can't strip in
// place, such as a HEIC, into a JPEG or PNG: upright, scaled to fit max_side
// if that's above 0, with its ICC profile and nothing else.
DM_EXPORT(dm_still) int32_t dm_still(int32_t max_side, int32_t quality) {
    AVFormatContext *ic = NULL;
    AVIOContext *io = NULL;
    AVFrame *frame = av_frame_alloc();
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);
    int ret = frame ? open_input(&ic, &io) : AVERROR(ENOMEM);
    if (ret < 0) {
        goto end;
    }
    const AVStreamGroup *grid = NULL;
    for (unsigned i = 0; i < ic->nb_stream_groups; i++) {
        if (ic->stream_groups[i]->type == AV_STREAM_GROUP_PARAMS_TILE_GRID) {
            grid = ic->stream_groups[i];
            break;
        }
    }
    const AVPacketSideData *sd = NULL, *icc = NULL;
    int nb = 0, placed = 0;
    AVRational sar = {0, 1};
    av_bprintf(&b, "{");
    if (grid) {
        if ((ret = decode_grid(ic, grid, frame, &placed)) < 0) {
            goto end;
        }
        sd = grid->params.tile_grid->coded_side_data;
        nb = grid->params.tile_grid->nb_coded_side_data;
        if (!side_data(sd, nb, AV_PKT_DATA_ICC_PROFILE) && grid->nb_streams) {
            icc = side_data(grid->streams[0]->codecpar->coded_side_data,
                            grid->streams[0]->codecpar->nb_coded_side_data, AV_PKT_DATA_ICC_PROFILE);
        }
        av_bprintf(&b, "\"tiles\":%u,\"tiles_placed\":%d,", grid->params.tile_grid->nb_tiles, placed);
    } else {
        int st = picture_stream(ic);
        if (st < 0) {
            ret = st;
            goto end;
        }
        if ((ret = decode_first(ic, st, frame)) < 0) {
            goto end;
        }
        sd = ic->streams[st]->codecpar->coded_side_data;
        nb = ic->streams[st]->codecpar->nb_coded_side_data;
        sar = stream_sar(ic->streams[st]);
        if ((ret = crop_as_stored(frame, sd, nb)) < 0) {
            goto end;
        }
    }
    if (!icc) {
        icc = side_data(sd, nb, AV_PKT_DATA_ICC_PROFILE);
    }
    // Decoders of formats that carry their own profile, such as TIFF, put it
    // on the frame.
    const AVFrameSideData *ficc = av_frame_get_side_data(frame, AV_FRAME_DATA_ICC_PROFILE);
    const uint8_t *icc_data = icc ? icc->data : ficc ? ficc->data : NULL;
    int icc_size = icc ? (int)icc->size : ficc ? (int)ficc->size : 0;
    av_bprintf(&b, "\"source_width\":%d,\"source_height\":%d,", frame->width, frame->height);
    ret = encode_image(frame, sar.num ? sar : frame->sample_aspect_ratio, display_matrix(sd, nb), max_side, quality,
                       icc_data, icc_size, &b);
    av_bprintf(&b, "}");

end:
    av_frame_free(&frame);
    avformat_close_input(&ic);
    close_io(&io);
    return finish(&b, ret);
}

// dm_poster makes a video's preview: its first frame, cropped, stretched and
// turned as its container asks, as a JPEG fitting max_side.
DM_EXPORT(dm_poster) int32_t dm_poster(int32_t max_side, int32_t quality) {
    AVFormatContext *ic = NULL;
    AVIOContext *io = NULL;
    AVFrame *frame = av_frame_alloc();
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);
    int ret = frame ? open_input(&ic, &io) : AVERROR(ENOMEM);
    if (ret < 0) {
        goto end;
    }
    int st = picture_stream(ic);
    if (st < 0) {
        ret = st;
        goto end;
    }
    if ((ret = decode_first(ic, st, frame)) < 0) {
        goto end;
    }
    const AVCodecParameters *par = ic->streams[st]->codecpar;
    if ((ret = crop_as_stored(frame, par->coded_side_data, par->nb_coded_side_data)) < 0) {
        goto end;
    }
    av_bprintf(&b, "{\"source_width\":%d,\"source_height\":%d,", frame->width, frame->height);
    AVRational sar = stream_sar(ic->streams[st]);
    ret = encode_image(frame, sar.num ? sar : frame->sample_aspect_ratio,
                       display_matrix(par->coded_side_data, par->nb_coded_side_data), max_side, quality, NULL, 0, &b);
    av_bprintf(&b, "}");

end:
    av_frame_free(&frame);
    avformat_close_input(&ic);
    close_io(&io);
    return finish(&b, ret);
}

DM_EXPORT(dm_error) int32_t dm_error(int32_t err, char *buf, int32_t size) {
    return av_strerror(err, buf, (size_t)size);
}
