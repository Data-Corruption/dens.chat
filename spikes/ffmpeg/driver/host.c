// A native host for the driver, the baseline the WebAssembly build is measured
// against: dmnative probe IN, dmnative strip MUXER IN OUT, or dmnative still|poster
// IN OUT MAX_SIDE QUALITY.

#define _FILE_OFFSET_BITS 64

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "host.h"

int32_t dm_probe(void);
int32_t dm_strip(const char *muxer);
int32_t dm_still(int32_t max_side, int32_t quality);
int32_t dm_poster(int32_t max_side, int32_t quality);
void dm_init(int32_t level);
int32_t dm_error(int32_t err, char *buf, int32_t size);

static FILE *files[2];

int32_t host_read(int32_t file, uint8_t *buf, int32_t size) {
    size_t n = fread(buf, 1, (size_t)size, files[file]);
    return n == 0 && ferror(files[file]) ? -1 : (int32_t)n;
}

int32_t host_write(int32_t file, const uint8_t *buf, int32_t size) {
    return (int32_t)fwrite(buf, 1, (size_t)size, files[file]);
}

int64_t host_seek(int32_t file, int64_t offset, int32_t whence) {
    FILE *f = files[file];
    if (whence == HOST_SEEK_SIZE) {
        off_t pos = ftello(f);
        if (fseeko(f, 0, SEEK_END) != 0) {
            return -1;
        }
        off_t size = ftello(f);
        fseeko(f, pos, SEEK_SET);
        return size;
    }
    if (fseeko(f, (off_t)offset, whence) != 0) {
        return -1;
    }
    return ftello(f);
}

void host_log(int32_t level, const char *msg, int32_t len) {
    (void)level;
    fwrite(msg, 1, (size_t)len, stderr);
}

void host_result(const char *data, int32_t len) {
    fwrite(data, 1, (size_t)len, stdout);
    fputc('\n', stdout);
}

int main(int argc, char **argv) {
    int32_t ret;
    dm_init(24); // AV_LOG_WARNING
    if (argc == 3 && strcmp(argv[1], "probe") == 0) {
        files[0] = fopen(argv[2], "rb");
        ret = files[0] ? dm_probe() : -1;
    } else if (argc == 5 && strcmp(argv[1], "strip") == 0) {
        files[0] = fopen(argv[3], "rb");
        files[1] = fopen(argv[4], "w+b");
        ret = files[0] && files[1] ? dm_strip(argv[2]) : -1;
        if (files[1] && fclose(files[1]) != 0) {
            ret = -1;
        }
    } else if (argc == 6 && (strcmp(argv[1], "still") == 0 || strcmp(argv[1], "poster") == 0)) {
        files[0] = fopen(argv[2], "rb");
        files[1] = fopen(argv[3], "w+b");
        int max_side = atoi(argv[4]), quality = atoi(argv[5]);
        ret = !files[0] || !files[1] ? -1 : argv[1][0] == 's' ? dm_still(max_side, quality) : dm_poster(max_side, quality);
        if (files[1] && fclose(files[1]) != 0) {
            ret = -1;
        }
    } else {
        fprintf(stderr, "usage: dmnative probe IN | strip MUXER IN OUT | still|poster IN OUT MAX_SIDE QUALITY\n");
        return 2;
    }
    if (ret < 0) {
        char msg[256];
        dm_error(ret, msg, sizeof(msg));
        fprintf(stderr, "error: %s\n", msg);
        return 1;
    }
    return 0;
}
