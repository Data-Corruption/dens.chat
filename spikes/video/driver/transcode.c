// The video spike's transcoder (spikes/video): a phone video's smaller copy
// made in the media module, to measure what libaom's realtime AV1 encoder
// costs there. It decodes the input's video, keeps at most fps frames a
// second, scales each to fit max_side as it's stored, unturned, and encodes
// it with libaom at kbps into Matroska. With speed below 0 it only decodes
// and scales, which splits the time between the two. It links beside the
// media module's own driver, whose exports it leaves alone.

#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/bprint.h>
#include <libavutil/opt.h>
#include <libswscale/swscale.h>

#include "host.h"

#define IO_BUFFER (64 * 1024)

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
        io = avio_alloc_context(buf, IO_BUFFER, writable, h, writable ? NULL : io_read, writable ? io_write : NULL,
                                io_seek);
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

// scale_flags picks swscale's filter, VS_SCALE naming another than
// bilinear.
static int scale_flags(void) {
    const char *s = getenv("VS_SCALE");
    if (s && !strcmp(s, "bicubic")) {
        return SWS_BICUBIC;
    }
    if (s && !strcmp(s, "area")) {
        return SWS_AREA;
    }
    if (s && !strcmp(s, "fast")) {
        return SWS_FAST_BILINEAR;
    }
    return SWS_BILINEAR;
}

// fit_size gives the longer side at most max_side, in even pixels, never
// enlarged.
static void fit_size(int w, int h, int max_side, int *ow, int *oh) {
    double k = FFMAX(w, h) > max_side ? (double)max_side / FFMAX(w, h) : 1;
    *ow = FFMAX(2, (int)lround(w * k / 2) * 2);
    *oh = FFMAX(2, (int)lround(h * k / 2) * 2);
}

static int drain(AVCodecContext *enc, AVFormatContext *oc, AVStream *ost, AVPacket *pkt, int *written, int64_t *bytes) {
    int ret;
    while ((ret = avcodec_receive_packet(enc, pkt)) >= 0) {
        *bytes += pkt->size;
        (*written)++;
        av_packet_rescale_ts(pkt, enc->time_base, ost->time_base);
        pkt->stream_index = ost->index;
        if ((ret = av_interleaved_write_frame(oc, pkt)) < 0) {
            return ret;
        }
    }
    return ret == AVERROR(EAGAIN) || ret == AVERROR_EOF ? 0 : ret;
}

DM_EXPORT(vs_transcode)
int32_t vs_transcode(int32_t max_side, int32_t fps, int32_t kbps, int32_t speed, int32_t max_frames, int32_t ten_bit) {
    AVFormatContext *ic = NULL, *oc = NULL;
    AVIOContext *in = NULL, *out = NULL;
    AVCodecContext *dec = NULL, *enc = NULL;
    struct SwsContext *sws = NULL;
    AVDictionary *opts = NULL;
    AVPacket *pkt = av_packet_alloc(), *opkt = av_packet_alloc();
    AVFrame *frame = av_frame_alloc(), *scaled = av_frame_alloc();
    AVStream *ost = NULL;
    AVBPrint b;
    av_bprint_init(&b, 0, AV_BPRINT_SIZE_UNLIMITED);
    int ret, decoded = 0, kept = 0, written = 0;
    int64_t bytes = 0;
    int encode = speed >= 0;

    in = open_io(HOST_INPUT, 0);
    ic = avformat_alloc_context();
    if (!pkt || !opkt || !frame || !scaled || !in || !ic || fps < 1) {
        ret = AVERROR(ENOMEM);
        goto end;
    }
    ic->pb = in;
    if ((ret = avformat_open_input(&ic, NULL, NULL, NULL)) < 0) {
        goto end;
    }
    avformat_find_stream_info(ic, NULL);
    int st = av_find_best_stream(ic, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
    if (st < 0) {
        ret = st;
        goto end;
    }
    AVStream *ist = ic->streams[st];
    const AVCodec *dc = avcodec_find_decoder(ist->codecpar->codec_id);
    if (!dc || !(dec = avcodec_alloc_context3(dc))) {
        ret = AVERROR_DECODER_NOT_FOUND;
        goto end;
    }
    dec->thread_count = 1;
    if ((ret = avcodec_parameters_to_context(dec, ist->codecpar)) < 0 || (ret = avcodec_open2(dec, dc, NULL)) < 0) {
        goto end;
    }
    dec->pkt_timebase = ist->time_base;
    // A video of more than half again the frames kept skips decoding the
    // frames nothing refers to, as phones' 60-frame video carries every
    // other frame.
    AVRational rate = ist->avg_frame_rate;
    int skip = getenv("VS_NOSKIP") == NULL && rate.num > 0 && rate.den > 0 && av_q2d(rate) > fps * 1.5;
    if (skip) {
        dec->skip_frame = AVDISCARD_NONREF;
    }

    int ow, oh;
    fit_size(dec->width, dec->height, max_side, &ow, &oh);
    enum AVPixelFormat fmt = ten_bit ? AV_PIX_FMT_YUV420P10 : AV_PIX_FMT_YUV420P;
    if (encode) {
        const AVCodec *ec = avcodec_find_encoder_by_name("libaom-av1");
        if (!ec || !(enc = avcodec_alloc_context3(ec))) {
            ret = AVERROR_ENCODER_NOT_FOUND;
            goto end;
        }
        enc->width = ow;
        enc->height = oh;
        enc->pix_fmt = fmt;
        enc->time_base = (AVRational){1, fps};
        enc->framerate = (AVRational){fps, 1};
        enc->bit_rate = (int64_t)kbps * 1000;
        enc->gop_size = fps * 5;
        enc->thread_count = 1;
        enc->color_range = AVCOL_RANGE_MPEG;
        enc->color_primaries = dec->color_primaries;
        enc->color_trc = dec->color_trc;
        enc->colorspace = dec->colorspace;
        av_dict_set(&opts, "usage", "realtime", 0);
        av_dict_set_int(&opts, "cpu-used", FFMIN(speed, 8), 0);
        av_dict_set(&opts, "row-mt", "0", 0);
        if (speed > 8) {
            char p[32];
            snprintf(p, sizeof p, "cpu-used=%d", speed);
            av_dict_set(&opts, "aom-params", p, 0);
        }
        if (!(out = open_io(HOST_OUTPUT, 1))) {
            ret = AVERROR(ENOMEM);
            goto end;
        }
        if ((ret = avformat_alloc_output_context2(&oc, NULL, "matroska", NULL)) < 0) {
            goto end;
        }
        oc->pb = out;
        if (oc->oformat->flags & AVFMT_GLOBALHEADER) {
            enc->flags |= AV_CODEC_FLAG_GLOBAL_HEADER;
        }
        if ((ret = avcodec_open2(enc, ec, &opts)) < 0) {
            goto end;
        }
        if (!(ost = avformat_new_stream(oc, NULL))) {
            ret = AVERROR(ENOMEM);
            goto end;
        }
        if ((ret = avcodec_parameters_from_context(ost->codecpar, enc)) < 0) {
            goto end;
        }
        ost->time_base = enc->time_base;
        if ((ret = avformat_write_header(oc, NULL)) < 0) {
            goto end;
        }
    }
    scaled->format = fmt;
    scaled->width = ow;
    scaled->height = oh;
    if ((ret = av_frame_get_buffer(scaled, 0)) < 0) {
        goto end;
    }

    // A frame goes once its time reaches the next slot, a 1/fps second
    // after the last one kept, give or take a quarter of a slot of jitter.
    int64_t slot = av_rescale_q(1, (AVRational){1, fps}, ist->time_base), next = AV_NOPTS_VALUE;
    for (int eof = 0; !eof && (max_frames <= 0 || kept < max_frames);) {
        int r = av_read_frame(ic, pkt);
        if (r < 0) {
            eof = 1;
            avcodec_send_packet(dec, NULL);
        } else if (pkt->stream_index != st) {
            av_packet_unref(pkt);
            continue;
        } else {
            avcodec_send_packet(dec, pkt);
            av_packet_unref(pkt);
        }
        while (avcodec_receive_frame(dec, frame) >= 0) {
            decoded++;
            int64_t pts = frame->best_effort_timestamp;
            if (pts != AV_NOPTS_VALUE && next != AV_NOPTS_VALUE && pts < next - slot / 4) {
                av_frame_unref(frame);
                continue;
            }
            if (pts != AV_NOPTS_VALUE) {
                next = pts + slot;
            }
            if (!sws && !(sws = sws_getContext(frame->width, frame->height, frame->format, ow, oh, fmt, scale_flags(),
                                               NULL, NULL, NULL))) {
                ret = AVERROR(EINVAL);
                goto end;
            }
            if ((ret = av_frame_make_writable(scaled)) < 0) {
                goto end;
            }
            sws_scale(sws, (const uint8_t *const *)frame->data, frame->linesize, 0, frame->height, scaled->data,
                      scaled->linesize);
            av_frame_unref(frame);
            scaled->pts = kept++;
            if (encode) {
                if ((ret = avcodec_send_frame(enc, scaled)) < 0 || (ret = drain(enc, oc, ost, opkt, &written, &bytes)) < 0) {
                    goto end;
                }
            }
            if (max_frames > 0 && kept >= max_frames) {
                break;
            }
        }
    }
    if (encode) {
        if ((ret = avcodec_send_frame(enc, NULL)) < 0 || (ret = drain(enc, oc, ost, opkt, &written, &bytes)) < 0 ||
            (ret = av_write_trailer(oc)) < 0) {
            goto end;
        }
    }
    ret = 0;
    av_bprintf(&b, "{\"decoded\":%d,\"kept\":%d,\"written\":%d,\"bytes\":%lld,\"width\":%d,\"height\":%d,\"skip\":%d}",
               decoded, kept, written, (long long)bytes, ow, oh, skip);

end:
    av_dict_free(&opts);
    sws_freeContext(sws);
    av_frame_free(&frame);
    av_frame_free(&scaled);
    av_packet_free(&pkt);
    av_packet_free(&opkt);
    avcodec_free_context(&dec);
    avcodec_free_context(&enc);
    avformat_close_input(&ic);
    close_io(&in);
    if (oc) {
        avformat_free_context(oc);
    }
    close_io(&out);
    if (ret >= 0 && !av_bprint_is_complete(&b)) {
        ret = AVERROR(ENOMEM);
    }
    if (ret >= 0) {
        host_result(b.str, (int32_t)b.len);
    }
    av_bprint_finalize(&b, NULL);
    return ret;
}
