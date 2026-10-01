// What the driver needs from its host. In WebAssembly these are the module's
// imports, and the only way out of it; natively host.c provides them.

#ifndef DM_HOST_H
#define DM_HOST_H

#include <stdint.h>

enum { HOST_INPUT = 0, HOST_OUTPUT = 1 };

// host_seek's whence: SEEK_SET, SEEK_CUR and SEEK_END, or this for the size.
#define HOST_SEEK_SIZE 3

#ifdef __wasm__
#define HOST_IMPORT(name) __attribute__((import_module("dens"), import_name(#name)))
#define DM_EXPORT(name) __attribute__((export_name(#name)))
#else
#define HOST_IMPORT(name)
#define DM_EXPORT(name)
#endif

// host_read returns the bytes read, 0 at the end, or a negative error.
HOST_IMPORT(read) int32_t host_read(int32_t file, uint8_t *buf, int32_t size);
HOST_IMPORT(write) int32_t host_write(int32_t file, const uint8_t *buf, int32_t size);
HOST_IMPORT(seek) int64_t host_seek(int32_t file, int64_t offset, int32_t whence);
HOST_IMPORT(log) void host_log(int32_t level, const char *msg, int32_t len);
// host_result takes an operation's answer, as JSON.
HOST_IMPORT(result) void host_result(const char *data, int32_t len);

#endif
