// The spike's driver: what Dens would do with media, over FFmpeg's libraries.
// It does all its I/O through functions the host provides, so the same code
// runs as a WebAssembly module translated to Go, and natively (host.c).

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
// only what playback needs, and the ICC profile, which Dens keeps in images
// too, so anything else the container attached, such as a HEIF image's EXIF,
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
        // As the ffmpeg command copies: the container's aspect ratio when it
        // states one, for the stream and the codec alike, which muxers check.
        AVRational sar = ist->sample_aspect_ratio.num ? ist->sample_aspect_ratio : ist->codecpar->sample_aspect_ratio;
        ost->sample_aspect_ratio = ost->codecpar->sample_aspect_ratio = sar;
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

// Stills and posters.

// The ways a display matrix can ask for an image to be turned, as the ffmpeg
// command reads it: none, flips, and the four turns that swap width and height.
enum turn { TURN_NONE, TURN_HFLIP, TURN_VFLIP, TURN_180, TURN_TRANSPOSE, TURN_CLOCK, TURN_CCLOCK, TURN_CLOCK_FLIP };

static const char *const turn_names[] = {"none", "hflip", "vflip", "180", "transpose", "clock", "cclock", "clock_flip"};

static int turn_swaps(enum turn t) {
    return t >= TURN_TRANSPOSE;
}

// turn_for follows fftools/ffmpeg_filter.c, so a still comes out the way
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

static const AVPacketSideData *side_data(const AVPacketSideData *sd, int nb, enum AVPacketSideDataType type) {
    return av_packet_side_data_get(sd, nb, type);
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

// encode_image scales a frame to fit max_side (0 keeps its size), turns it,
// and encodes it as a JPEG, or a PNG when it has transparency, carrying only
// the ICC profile given. The encoders get a fresh frame, so nothing else the
// source carried, EXIF included, reaches them. It empties src once it's
// scaled, and holds at most two full images at a time: a 48-megapixel photo
// is 73 MB in each.
static int encode_image(AVFrame *src, const int32_t *matrix, int max_side, int quality, const uint8_t *icc,
                        int icc_size, AVBPrint *b) {
    const AVPixFmtDescriptor *desc = av_pix_fmt_desc_get(src->format);
    int alpha = desc && (desc->flags & AV_PIX_FMT_FLAG_ALPHA);
    enum turn t = turn_for(matrix);
    int sw = src->width, sh = src->height;
    int ow = turn_swaps(t) ? sh : sw, oh = turn_swaps(t) ? sw : sh;
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
    sws_setColorspaceDetails(sws, sws_getCoefficients(src->colorspace == AVCOL_SPC_UNSPECIFIED ? SWS_CS_DEFAULT : src->colorspace),
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
    if (host_write(HOST_OUTPUT, pkt->data, pkt->size) != pkt->size) {
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

// decode_first returns the first frame decoded from stream st.
static int decode_first(AVFormatContext *ic, int st, AVFrame *frame) {
    AVCodecContext *dec = open_decoder(ic->streams[st]->codecpar);
    AVPacket *pkt = av_packet_alloc();
    int ret = dec && pkt ? AVERROR(EAGAIN) : AVERROR_DECODER_NOT_FOUND;
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
    int ret = tile_of && dec && pkt && tile ? 0 : AVERROR(ENOMEM);
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
    const AVPacketSideData *sd = NULL, *matrix = NULL, *icc = NULL;
    int nb = 0, placed = 0, tiles = 0;
    av_bprintf(&b, "{");
    if (grid) {
        tiles = grid->params.tile_grid->nb_tiles;
        if ((ret = decode_grid(ic, grid, frame, &placed)) < 0) {
            goto end;
        }
        sd = grid->params.tile_grid->coded_side_data;
        nb = grid->params.tile_grid->nb_coded_side_data;
        if (!side_data(sd, nb, AV_PKT_DATA_ICC_PROFILE) && grid->nb_streams) {
            icc = side_data(grid->streams[0]->codecpar->coded_side_data,
                            grid->streams[0]->codecpar->nb_coded_side_data, AV_PKT_DATA_ICC_PROFILE);
        }
        av_bprintf(&b, "\"tiles\":%d,\"tiles_placed\":%d,", tiles, placed);
    } else {
        int st = av_find_best_stream(ic, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
        if (st < 0) {
            ret = st;
            goto end;
        }
        if ((ret = decode_first(ic, st, frame)) < 0) {
            goto end;
        }
        sd = ic->streams[st]->codecpar->coded_side_data;
        nb = ic->streams[st]->codecpar->nb_coded_side_data;
        if ((ret = crop_as_stored(frame, sd, nb)) < 0) {
            goto end;
        }
    }
    matrix = side_data(sd, nb, AV_PKT_DATA_DISPLAYMATRIX);
    if (!icc) {
        icc = side_data(sd, nb, AV_PKT_DATA_ICC_PROFILE);
    }
    // Decoders of formats that carry their own profile, such as TIFF, put it
    // on the frame.
    const AVFrameSideData *ficc = av_frame_get_side_data(frame, AV_FRAME_DATA_ICC_PROFILE);
    const uint8_t *icc_data = icc ? icc->data : ficc ? ficc->data : NULL;
    int icc_size = icc ? (int)icc->size : ficc ? (int)ficc->size : 0;
    av_bprintf(&b, "\"source\":\"%dx%d %s\",", frame->width, frame->height, av_get_pix_fmt_name(frame->format));
    ret = encode_image(frame, matrix && matrix->size >= 36 ? (const int32_t *)matrix->data : NULL, max_side, quality,
                       icc_data, icc_size, &b);
    av_bprintf(&b, "}");

end:
    av_frame_free(&frame);
    avformat_close_input(&ic);
    close_io(&io);
    return finish(&b, ret);
}

// dm_poster makes a video's preview: its first frame, upright, as a JPEG
// fitting max_side.
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
    int st = av_find_best_stream(ic, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
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
    const AVPacketSideData *matrix = side_data(par->coded_side_data, par->nb_coded_side_data, AV_PKT_DATA_DISPLAYMATRIX);
    av_bprintf(&b, "{\"source\":\"%dx%d %s\",", frame->width, frame->height, av_get_pix_fmt_name(frame->format));
    ret = encode_image(frame, matrix && matrix->size >= 36 ? (const int32_t *)matrix->data : NULL, max_side, quality,
                       NULL, 0, &b);
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
