// The client's cache (M6): what this computer keeps of other dens' files,
// in the words and sizes the settings use for it.

import { formatSize } from './files.jsx';

const MiB = 1024 ** 2;
const GiB = 1024 ** 3;

// CACHE_SIZES are the sizes the member chooses the cache's from, which the
// local service holds it to: 2 GiB until they choose.
export const CACHE_SIZES = [512 * MiB, 1 * GiB, 2 * GiB, 5 * GiB, 10 * GiB, 20 * GiB, 50 * GiB];

// cacheSizes are the choices to show for a cache of limit bytes: the usual
// ones, and its own if it's another.
export function cacheSizes(limit) {
    if (!limit || CACHE_SIZES.includes(limit)) return CACHE_SIZES;
    return [...CACHE_SIZES, limit].sort((a, b) => a - b);
}

// cacheUsage says how much of its size the cache holds.
export function cacheUsage(used, limit) {
    if (!used) return 'It holds nothing now.';
    return `It holds ${formatSize(used)} of ${formatSize(limit)} now.`;
}
