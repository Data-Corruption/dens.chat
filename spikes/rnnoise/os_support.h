/* RNNoise 0.2's generic vector code, which WebAssembly builds use, includes
   Opus's os_support.h, which RNNoise doesn't carry, for OPUS_CLEAR alone. */
#ifndef OS_SUPPORT_H
#define OS_SUPPORT_H

#include <string.h>

#define OPUS_CLEAR(dst, n) (memset((dst), 0, (n) * sizeof(*(dst))))

#endif
