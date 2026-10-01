// A native host for the driver, for fuzzing it under AddressSanitizer beside
// the module: the same C, FFmpeg's and Dens's, with the memory checks the
// module can't make (FuzzDriver, in ../fuzz_test.go). Dens never ships it.
//
//   dm-native probe IN
//   dm-native strip IN OUT
//   dm-native still|poster IN OUT MAX_SIDE QUALITY

#define _FILE_OFFSET_BITS 64
#define _GNU_SOURCE

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "host.h"

void dm_init(int32_t level);
int32_t dm_probe(void);
int32_t dm_strip(const char *muxer);
int32_t dm_still(int32_t max_side, int32_t quality);
int32_t dm_poster(int32_t max_side, int32_t quality);
int32_t dm_error(int32_t err, char *buf, int32_t size);

static int fds[2] = {-1, -1};

int32_t host_read(int32_t file, uint8_t *buf, int32_t size, int64_t offset) {
    if (file < 0 || file > 1 || fds[file] < 0 || size < 0 || offset < 0) {
        return -1;
    }
    ssize_t n = pread(fds[file], buf, (size_t)size, (off_t)offset);
    return n < 0 ? -1 : (int32_t)n;
}

int32_t host_write(int32_t file, const uint8_t *buf, int32_t size, int64_t offset) {
    if (file != HOST_OUTPUT || fds[file] < 0 || size < 0 || offset < 0) {
        return -1;
    }
    for (int32_t done = 0; done < size;) {
        ssize_t n = pwrite(fds[file], buf + done, (size_t)(size - done), (off_t)(offset + done));
        if (n <= 0) {
            return -1;
        }
        done += (int32_t)n;
    }
    return size;
}

int64_t host_size(int32_t file) {
    struct stat st;
    if (file < 0 || file > 1 || fds[file] < 0 || fstat(fds[file], &st) != 0) {
        return -1;
    }
    return st.st_size;
}

void host_log(int32_t level, const char *msg, int32_t len) {
    (void)level;
    (void)msg;
    (void)len;
}

void host_result(const char *data, int32_t len) {
    fwrite(data, 1, (size_t)len, stdout);
    fputc('\n', stdout);
}

int main(int argc, char **argv) {
    dm_init(-8); // AV_LOG_QUIET
    int32_t ret;
    if (argc == 3 && strcmp(argv[1], "probe") == 0) {
        fds[0] = open(argv[2], O_RDONLY);
        ret = fds[0] < 0 ? -1 : dm_probe();
    } else if (argc == 4 && strcmp(argv[1], "strip") == 0) {
        fds[0] = open(argv[2], O_RDONLY);
        fds[1] = open(argv[3], O_RDWR | O_CREAT | O_TRUNC, 0600);
        ret = fds[0] < 0 || fds[1] < 0 ? -1 : dm_strip("");
    } else if (argc == 6 && (strcmp(argv[1], "still") == 0 || strcmp(argv[1], "poster") == 0)) {
        fds[0] = open(argv[2], O_RDONLY);
        fds[1] = open(argv[3], O_RDWR | O_CREAT | O_TRUNC, 0600);
        int32_t max_side = atoi(argv[4]), quality = atoi(argv[5]);
        ret = fds[0] < 0 || fds[1] < 0 ? -1
              : argv[1][1] == 't'      ? dm_still(max_side, quality)
                                       : dm_poster(max_side, quality);
    } else {
        fprintf(stderr, "usage: dm-native probe IN | strip IN OUT | still|poster IN OUT MAX_SIDE QUALITY\n");
        return 2;
    }
    if (ret < 0) {
        char msg[128] = "";
        dm_error(ret, msg, sizeof msg);
        fprintf(stderr, "%s\n", msg);
        return 1;
    }
    return 0;
}
