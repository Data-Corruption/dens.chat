// A native host for the spike's transcoder, beside the module: the same C,
// built with the system's compiler, timed the same way.
//
//   vs-native IN OUT MAX_SIDE FPS KBPS SPEED MAX_FRAMES TEN_BIT

#define _FILE_OFFSET_BITS 64
#define _GNU_SOURCE

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#include "host.h"

void dm_init(int32_t level);
int32_t dm_error(int32_t err, char *buf, int32_t size);
int32_t vs_transcode(int32_t max_side, int32_t fps, int32_t kbps, int32_t speed, int32_t max_frames, int32_t ten_bit);

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
    fwrite(msg, 1, (size_t)len, stderr);
}

void host_result(const char *data, int32_t len) {
    fwrite(data, 1, (size_t)len, stdout);
}

int main(int argc, char **argv) {
    if (argc != 9) {
        fprintf(stderr, "usage: vs-native IN OUT MAX_SIDE FPS KBPS SPEED MAX_FRAMES TEN_BIT\n");
        return 2;
    }
    dm_init(16); // AV_LOG_ERROR
    fds[0] = open(argv[1], O_RDONLY);
    fds[1] = open(argv[2], O_RDWR | O_CREAT | O_TRUNC, 0600);
    if (fds[0] < 0 || fds[1] < 0) {
        perror("open");
        return 1;
    }
    struct timespec a, b;
    clock_gettime(CLOCK_MONOTONIC, &a);
    int32_t ret = vs_transcode(atoi(argv[3]), atoi(argv[4]), atoi(argv[5]), atoi(argv[6]), atoi(argv[7]), atoi(argv[8]));
    clock_gettime(CLOCK_MONOTONIC, &b);
    if (ret < 0) {
        char msg[128] = "";
        dm_error(ret, msg, sizeof msg);
        fprintf(stderr, "%s\n", msg);
        return 1;
    }
    printf(" %.3f\n", (double)(b.tv_sec - a.tv_sec) + (double)(b.tv_nsec - a.tv_nsec) / 1e9);
    return 0;
}
