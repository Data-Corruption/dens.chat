// setjmp.h for libaom in the media module, which has no exception handling
// to unwind with. libaom longjmps only out of an internal error, so setjmp
// returns 0 and longjmp traps: the error ends the job, as any trap does.

#ifndef VS_SETJMP_H
#define VS_SETJMP_H

typedef int jmp_buf[1];

#define setjmp(env) ((void)(env), 0)
#define longjmp(env, val) ((void)(env), (void)(val), __builtin_trap())

#endif
