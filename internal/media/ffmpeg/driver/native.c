// A native host for the driver, for fuzzing it under AddressSanitizer beside
// the module: the same C, FFmpeg's and Dens's, with the memory checks the
// module can't make (FuzzDriver, in ../fuzz_test.go). Dens never ships it.
//
//   dm-native probe IN
//   dm-native strip IN OUT
//   dm-native still IN OUT MAX_SIDE QUALITY ORIENTATION
//   dm-native poster IN OUT MAX_SIDE QUALITY
//   dm-native scan IN
//   dm-native encode IN OUT START_US END_US MAX_SIDE FPS KBPS
//   dm-native mux IN PACKETS OUT WIDTH HEIGHT
//   dm-native demux IN OUT

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
int32_t dm_still(int32_t max_side, int32_t quality, int32_t orientation);
int32_t dm_poster(int32_t max_side, int32_t quality);
int32_t dm_scan(void);
int32_t dm_encode(int64_t start_us, int64_t end_us, int32_t max_side, int32_t fps, int32_t kbps);
int32_t dm_mux(int32_t width, int32_t height);
int32_t dm_demux(void);
int32_t dm_error(int32_t err, char *buf, int32_t size);

static int fds[3] = {-1, -1, -1};

int32_t host_read(int32_t file, uint8_t *buf, int32_t size, int64_t offset) {
    if (file < 0 || file > 2 || fds[file] < 0 || size < 0 || offset < 0) {
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
    if (file < 0 || file > 2 || fds[file] < 0 || fstat(fds[file], &st) != 0) {
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

void host_progress(int64_t done) { (void)done; }

static int open_out(const char *path) { return open(path, O_RDWR | O_CREAT | O_TRUNC, 0600); }

int main(int argc, char **argv) {
    dm_init(-8); // AV_LOG_QUIET
    int32_t ret;
    if (argc == 3 && strcmp(argv[1], "probe") == 0) {
        fds[0] = open(argv[2], O_RDONLY);
        ret = fds[0] < 0 ? -1 : dm_probe();
    } else if (argc == 4 && (strcmp(argv[1], "strip") == 0 || strcmp(argv[1], "demux") == 0)) {
        fds[0] = open(argv[2], O_RDONLY);
        fds[1] = open_out(argv[3]);
        ret = fds[0] < 0 || fds[1] < 0 ? -1 : argv[1][0] == 's' ? dm_strip("") : dm_demux();
    } else if ((argc == 7 && strcmp(argv[1], "still") == 0) || (argc == 6 && strcmp(argv[1], "poster") == 0)) {
        fds[0] = open(argv[2], O_RDONLY);
        fds[1] = open_out(argv[3]);
        int32_t max_side = atoi(argv[4]), quality = atoi(argv[5]);
        ret = fds[0] < 0 || fds[1] < 0 ? -1
              : argc == 7              ? dm_still(max_side, quality, atoi(argv[6]))
                                       : dm_poster(max_side, quality);
    } else if (argc == 3 && strcmp(argv[1], "scan") == 0) {
        fds[0] = open(argv[2], O_RDONLY);
        ret = fds[0] < 0 ? -1 : dm_scan();
    } else if (argc == 9 && strcmp(argv[1], "encode") == 0) {
        fds[0] = open(argv[2], O_RDONLY);
        fds[1] = open_out(argv[3]);
        ret = fds[0] < 0 || fds[1] < 0 ? -1
                                       : dm_encode(atoll(argv[4]), atoll(argv[5]), atoi(argv[6]), atoi(argv[7]), atoi(argv[8]));
    } else if (argc == 7 && strcmp(argv[1], "mux") == 0) {
        fds[0] = open(argv[2], O_RDONLY);
        fds[2] = open(argv[3], O_RDONLY);
        fds[1] = open_out(argv[4]);
        ret = fds[0] < 0 || fds[1] < 0 || fds[2] < 0 ? -1 : dm_mux(atoi(argv[5]), atoi(argv[6]));
    } else {
        fprintf(stderr, "usage: dm-native probe IN | strip IN OUT | still IN OUT MAX_SIDE QUALITY ORIENTATION | "
                        "poster IN OUT MAX_SIDE QUALITY | scan IN | encode IN OUT START_US END_US MAX_SIDE FPS KBPS | "
                        "mux IN PACKETS OUT WIDTH HEIGHT | demux IN OUT\n");
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
