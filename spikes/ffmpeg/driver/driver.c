// The spike's driver: what Dens would do with media, over FFmpeg's libraries.
// It does all its I/O through functions the host provides, so the same code
// runs as a WebAssembly module translated to Go, and natively (host.c).

#include <stdint.h>
#include <string.h>

#include <libavformat/avformat.h>
#include <libavutil/bprint.h>
#include <libavutil/display.h>

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

static int io_read(void *opaque, uint8_t *buf, int size) {
    int32_t n = host_read((int32_t)(intptr_t)opaque, buf, size);
    if (n == 0) {
        return AVERROR_EOF;
    }
    return n < 0 ? AVERROR(EIO) : n;
}

static int io_write(void *opaque, const uint8_t *buf, int size) {
    int32_t n = host_write((int32_t)(intptr_t)opaque, buf, size);
    return n == size ? n : AVERROR(EIO);
}

static int64_t io_seek(void *opaque, int64_t offset, int whence) {
    int32_t file = (int32_t)(intptr_t)opaque;
    if (whence & AVSEEK_SIZE) {
        int64_t size = host_seek(file, 0, HOST_SEEK_SIZE);
        return size < 0 ? AVERROR(ENOSYS) : size;
    }
    int64_t pos = host_seek(file, offset, whence & ~AVSEEK_FORCE);
    return pos < 0 ? AVERROR(EIO) : pos;
}

static AVIOContext *open_io(int32_t file, int writable) {
    uint8_t *buf = av_malloc(IO_BUFFER);
    if (!buf) {
        return NULL;
    }
    AVIOContext *io = avio_alloc_context(buf, IO_BUFFER, writable, (void *)(intptr_t)file,
                                         writable ? NULL : io_read, writable ? io_write : NULL, io_seek);
    if (!io) {
        av_free(buf);
    }
    return io;
}

static void close_io(AVIOContext **io) {
    if (*io) {
        av_freep(&(*io)->buffer);
        avio_context_free(io);
    }
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

static void put_keys(AVBPrint *b, const AVDictionary *m) {
    const AVDictionaryEntry *e = NULL;
    av_bprintf(b, "[");
    for (int i = 0; (e = av_dict_iterate(m, e)); i++) {
        av_bprintf(b, "%s\"", i ? "," : "");
        for (const char *c = e->key; *c; c++) {
            av_bprintf(b, *c == '"' || *c == '\\' ? "\\%c" : (uint8_t)*c < 0x20 ? "?" : "%c", *c);
        }
        av_bprintf(b, "\"");
    }
    av_bprintf(b, "]");
}

static double rotation(const AVCodecParameters *par) {
    const AVPacketSideData *sd = av_packet_side_data_get(par->coded_side_data, par->nb_coded_side_data,
                                                         AV_PKT_DATA_DISPLAYMATRIX);
    return sd && sd->size >= 9 * 4 ? av_display_rotation_get((const int32_t *)sd->data) : 0;
}

static void put_streams(AVBPrint *b, const AVFormatContext *ic) {
    av_bprintf(b, "\"streams\":[");
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        const AVStream *st = ic->streams[i];
        const AVCodecParameters *par = st->codecpar;
        av_bprintf(b, "%s{\"type\":\"%s\",\"codec\":\"%s\",\"tag\":\"%s\"", i ? "," : "",
                   av_get_media_type_string(par->codec_type) ? av_get_media_type_string(par->codec_type) : "unknown",
                   avcodec_get_name(par->codec_id), av_fourcc2str(par->codec_tag));
        if (par->codec_type == AVMEDIA_TYPE_VIDEO) {
            av_bprintf(b, ",\"width\":%d,\"height\":%d,\"rotation\":%g,\"attached_pic\":%s", par->width,
                       par->height, rotation(par), st->disposition & AV_DISPOSITION_ATTACHED_PIC ? "true" : "false");
        } else if (par->codec_type == AVMEDIA_TYPE_AUDIO) {
            av_bprintf(b, ",\"sample_rate\":%d,\"channels\":%d", par->sample_rate, par->ch_layout.nb_channels);
        }
        av_bprintf(b, ",\"side_data\":[");
        for (int j = 0; j < par->nb_coded_side_data; j++) {
            const char *name = av_packet_side_data_name(par->coded_side_data[j].type);
            av_bprintf(b, "%s\"%s\"", j ? "," : "", name ? name : "?");
        }
        av_bprintf(b, "],\"metadata\":");
        put_keys(b, st->metadata);
        av_bprintf(b, "}");
    }
    av_bprintf(b, "]");
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

// dm_probe describes the input: its container, its streams, and the names of
// its metadata, never their values.
DM_EXPORT(dm_probe) int32_t dm_probe(void) {
    AVFormatContext *ic = NULL;
    AVIOContext *io = NULL;
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);
    int ret = open_input(&ic, &io);
    if (ret >= 0) {
        av_bprintf(&b, "{\"format\":\"%s\",\"duration_us\":%" PRId64 ",\"chapters\":%u,\"metadata\":",
                   ic->iformat->name, ic->duration, ic->nb_chapters);
        put_keys(&b, ic->metadata);
        av_bprintf(&b, ",");
        put_streams(&b, ic);
        av_bprintf(&b, "}");
    }
    avformat_close_input(&ic);
    close_io(&io);
    return finish(&b, ret);
}

// keep says whether a stream goes into the stripped copy: video and audio,
// and nothing else. Subtitles go because some cameras write coordinates as
// subtitles; cover art, data and timed-metadata tracks go with the metadata.
static int keep(const AVStream *st, const AVOutputFormat *of) {
    enum AVMediaType type = st->codecpar->codec_type;
    if (type != AVMEDIA_TYPE_VIDEO && type != AVMEDIA_TYPE_AUDIO) {
        return 0;
    }
    if (st->disposition & AV_DISPOSITION_ATTACHED_PIC) {
        return 0;
    }
    // Some muxers can't say what they take, as Ogg with Opus; only a refusal
    // drops the stream, as with the ffmpeg command, which just tries.
    return avformat_query_codec(of, st->codecpar->codec_id, FF_COMPLIANCE_NORMAL) != 0;
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

// keep_side_data says whether a stream's side data goes into the stripped copy:
// only what playback needs, so anything else the container attached, such as
// a HEIF image's EXIF or an ICC profile's descriptions, stays behind.
static int keep_side_data(enum AVPacketSideDataType type) {
    switch (type) {
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

// The deepest frame reordering H.264 and HEVC allow.
#define MAX_REORDER 16

// reorder derives a stream's decode times from its presentation times, as
// FFmpeg's muxer once did: each packet's decode time is the earliest of the
// last MAX_REORDER + 1 presentation times, which only grows while the
// stream reorders no deeper than that.
typedef struct reorder {
    int64_t pts[MAX_REORDER + 1];
    int started;
} reorder;

static int64_t reorder_dts(reorder *r, int64_t pts) {
    if (!r->started) {
        for (int i = 0; i <= MAX_REORDER; i++) {
            r->pts[i] = pts;
        }
        r->started = 1;
    }
    r->pts[0] = pts;
    for (int i = 0; i < MAX_REORDER && r->pts[i] > r->pts[i + 1]; i++) {
        FFSWAP(int64_t, r->pts[i], r->pts[i + 1]);
    }
    return r->pts[0];
}

// dm_strip copies the input's video and audio into a new container of the
// named format, with none of the input's metadata, chapters or other streams.
// What playback needs travels with the codec parameters, rotation included.
DM_EXPORT(dm_strip) int32_t dm_strip(const char *muxer) {
    AVFormatContext *ic = NULL, *oc = NULL;
    AVIOContext *in = NULL, *out = NULL;
    AVPacket *pkt = av_packet_alloc();
    int *map = NULL;
    reorder *reorders = NULL;
    int kept = 0, dropped = 0, side_dropped = 0;
    unsigned nb_streams = 0;
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);

    int ret = pkt ? open_input(&ic, &in) : AVERROR(ENOMEM);
    if (ret < 0) {
        goto end;
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
    if (!out || !map || !reorders) {
        ret = AVERROR(ENOMEM);
        goto end;
    }
    oc->pb = out;
    // No "encoder" tag naming FFmpeg's version.
    oc->flags |= AVFMT_FLAG_BITEXACT;
    // Matroska keeps presentation times only. Without decoders, FFmpeg can't
    // tell how far a stream reorders its frames, so the decode times it infers
    // can run backwards; the driver derives its own.
    int pts_only = !strcmp(ic->iformat->name, "matroska,webm");

    for (unsigned i = 0; i < nb_streams; i++) {
        AVStream *ist = ic->streams[i];
        map[i] = -1;
        if (!keep(ist, oc->oformat)) {
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
        ost->sample_aspect_ratio = ist->sample_aspect_ratio;
        ost->disposition = ist->disposition & AV_DISPOSITION_DEFAULT;
        map[i] = ost->index;
        kept++;
    }
    if (!kept) {
        ret = AVERROR_STREAM_NOT_FOUND;
        goto end;
    }
    ret = avformat_write_header(oc, NULL);
    if (ret < 0) {
        goto end;
    }
    while ((ret = av_read_frame(ic, pkt)) >= 0) {
        int o = (unsigned)pkt->stream_index < nb_streams ? map[pkt->stream_index] : -1;
        if (o < 0) {
            av_packet_unref(pkt);
            continue;
        }
        if (pts_only && pkt->pts != AV_NOPTS_VALUE) {
            pkt->dts = reorder_dts(&reorders[pkt->stream_index], pkt->pts);
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
        av_bprintf(&b, "{\"kept\":%d,\"dropped\":%d,\"side_data_dropped\":%d,\"bytes\":%" PRId64 "}", kept,
                   dropped, side_dropped, avio_tell(out));
    }

end:
    av_packet_free(&pkt);
    av_free(map);
    av_free(reorders);
    avformat_close_input(&ic);
    close_io(&in);
    avformat_free_context(oc);
    close_io(&out);
    return finish(&b, ret);
}

DM_EXPORT(dm_error) int32_t dm_error(int32_t err, char *buf, int32_t size) {
    return av_strerror(err, buf, (size_t)size);
}
