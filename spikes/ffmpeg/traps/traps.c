// What a decoder taken over by a hostile file could do inside the module,
// one export each, built and translated as the driver is, so A4 can check
// that each ends the job and nothing else.

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define EXPORT(name) __attribute__((export_name(#name)))

static uint32_t memory_bytes(void) {
    return (uint32_t)__builtin_wasm_memory_size(0) * 65536u;
}

// Reads and writes at an address the caller picks, such as one past the
// module's memory.
EXPORT(load) int32_t load(uint32_t addr) {
    return *(volatile int32_t *)(uintptr_t)addr;
}

EXPORT(store) void store(uint32_t addr) {
    *(volatile int32_t *)(uintptr_t)addr = 0x41414141;
}

// Bulk memory operations that run n bytes past the end of the module's
// memory. wasi-libc uses memory.fill and memory.copy past a few bytes.
EXPORT(fill_past_end) int32_t fill_past_end(uint32_t n) {
    memset((void *)(uintptr_t)(memory_bytes() - 8), 0x41, 8 + n);
    return 0;
}

EXPORT(copy_past_end) int32_t copy_past_end(uint32_t n) {
    memmove((void *)(uintptr_t)(memory_bytes() - 8), (void *)(uintptr_t)1024, 8 + n);
    return 0;
}

// fresh_page_is_zero grows the memory by a page and reports whether the new
// page reads as zeros, as WebAssembly promises, after fill_past_end may have
// written there.
EXPORT(fresh_page_is_zero) int32_t fresh_page_is_zero(void) {
    uint32_t end = memory_bytes();
    if (__builtin_wasm_memory_grow(0, 1) < 0) {
        return -1;
    }
    for (uint32_t i = 0; i < 4096; i++) {
        if (((volatile uint8_t *)(uintptr_t)end)[i] != 0) {
            return 0;
        }
    }
    return 1;
}

typedef int32_t (*unary)(int32_t);
typedef int64_t (*binary)(int64_t, int64_t);

static int32_t twice(int32_t x) { return 2 * x; }

// Calls through a function pointer the caller picks: an index past the
// table's end, or one whose function has another signature.
EXPORT(call_index) int32_t call_index(uint32_t index) {
    volatile unary f = twice;
    f = (unary)(uintptr_t)index;
    return f(21);
}

EXPORT(call_wrong_type) int64_t call_wrong_type(void) {
    volatile uintptr_t p = (uintptr_t)twice;
    return ((binary)p)(1, 2);
}

EXPORT(unreachable) void unreachable(void) {
    __builtin_trap();
}

EXPORT(divide) int32_t divide(int32_t x, int32_t y) {
    volatile int32_t a = x, b = y;
    return a / b;
}

// Recursion that uses the shadow stack in linear memory, which comes first,
// so running out of it goes below address 0. Each frame escapes through sink,
// so the compiler must give it all 64 KB.
static volatile char *volatile sink;

EXPORT(shadow_stack) int32_t shadow_stack(int32_t depth) {
    char frame[64 * 1024];
    sink = frame;
    frame[0] = (char)depth;
    if (depth == 0) {
        return sink[0];
    }
    return shadow_stack(depth - 1) + frame[0];
}

// Recursion with no frame in linear memory, so only the Go stack grows: each
// call goes through a pointer the optimizer can't see through.
static int32_t deep(int32_t n);
static int32_t (*volatile next)(int32_t) = deep;

static int32_t deep(int32_t n) {
    if (n == 0) {
        return 0;
    }
    return next(n - 1) + 1;
}

EXPORT(go_stack) int32_t go_stack(int32_t depth) {
    return deep(depth);
}

// balloon takes memory 16 MB at a time, touching every page, until malloc
// refuses, and returns how many MB it got. Each block escapes through sink,
// so the compiler can't drop the allocations.
EXPORT(balloon) int32_t balloon(void) {
    int32_t mb = 0;
    for (;;) {
        volatile char *p = malloc(16 << 20);
        if (!p) {
            return mb;
        }
        for (size_t i = 0; i < (16u << 20); i += 4096) {
            p[i] = 1;
        }
        sink = p;
        mb += 16;
    }
}

EXPORT(spin) void spin(void) {
    volatile uint64_t n = 0;
    for (;;) {
        n++;
    }
}
