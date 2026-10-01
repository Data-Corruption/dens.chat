// What the driver needs from its host. In WebAssembly these are the module's
// imports, and the only way out of it.

#ifndef DM_HOST_H
#define DM_HOST_H

#include <stdint.h>

// The files a job has: the one it reads, and the one it writes.
enum { HOST_INPUT = 0, HOST_OUTPUT = 1 };

#ifdef __wasm__
#define HOST_IMPORT(name) __attribute__((import_module("dens"), import_name(#name)))
#define DM_EXPORT(name) __attribute__((export_name(#name)))
#else
#define HOST_IMPORT(name)
#define DM_EXPORT(name)
#endif

// host_read reads up to size bytes at offset, and returns how many it read,
// 0 at the end, or a negative error. It keeps no position: each caller
// keeps its own, as two contexts read and write one output at once.
HOST_IMPORT(read) int32_t host_read(int32_t file, uint8_t *buf, int32_t size, int64_t offset);
// host_write writes size bytes at offset, and returns size or a negative
// error.
HOST_IMPORT(write) int32_t host_write(int32_t file, const uint8_t *buf, int32_t size, int64_t offset);
// host_size returns a file's size, or a negative error.
HOST_IMPORT(size) int64_t host_size(int32_t file);
HOST_IMPORT(log) void host_log(int32_t level, const char *msg, int32_t len);
// host_result takes an operation's answer, as JSON.
HOST_IMPORT(result) void host_result(const char *data, int32_t len);

#endif
