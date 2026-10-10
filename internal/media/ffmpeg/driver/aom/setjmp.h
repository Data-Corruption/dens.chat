// setjmp.h for libaom in the media module. wasi-libc has no setjmp without
// WebAssembly's exception handling, which wasm2go doesn't translate. libaom
// longjmps only out of an internal error, so setjmp returns 0 and longjmp
// traps: the error ends the job, as any trap in the module does.

#ifndef DM_AOM_SETJMP_H
#define DM_AOM_SETJMP_H

typedef int jmp_buf[1];

#define setjmp(env) ((void)(env), 0)
#define longjmp(env, val) ((void)(env), (void)(val), __builtin_trap())

#endif
