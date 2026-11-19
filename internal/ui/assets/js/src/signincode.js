// Sign-in codes (M22.6) on the page: a scanned code's text read as Add
// device's, a den's address and a username, or as an approving device's
// digits, by the rules denproto.ReadSignInCode reads by and against the
// same cases; and the codes the service makes, checked and drawn.

const PREFIX = 'dens-device:';
const ADDRESS = /^(https?):\/\/(\[::1\]|[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*)(?::([1-9][0-9]{0,4}))?$/;
const LOOPBACK = /^(?:localhost|\[::1\]|127(?:\.(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])){3})$/;
const USERNAME = /^[a-z0-9_]{2,32}$/;
const DIGITS = /^[0-9]{16}$/;

/** readSignInCode reads a scanned code's text: { kind: 'device', url,
    username }, { kind: 'digits', digits }, or { kind: '' } for anything
    else, which is all a stranger's code can be. */
export function readSignInCode(text) {
    if (typeof text !== 'string') return { kind: '' };
    if (DIGITS.test(text)) return { kind: 'digits', digits: text };
    if (!text.startsWith(PREFIX)) return { kind: '' };
    const rest = text.slice(PREFIX.length);
    const space = rest.indexOf(' ');
    const url = rest.slice(0, space);
    const username = rest.slice(space + 1);
    if (space < 0 || !USERNAME.test(username) || !denAddress(url)) return { kind: '' };
    return { kind: 'device', url, username };
}

// denAddress says whether a code's address is one: https, or plain http
// on this computer, a host name or IPv4 address in lowercase, or [::1],
// maybe a port, and nothing after it.
function denAddress(s) {
    const m = ADDRESS.exec(s);
    if (!m || s.length > 200 || m[2].length > 253) return false;
    if (m[3] && Number(m[3]) > 65535) return false;
    return m[1] === 'https' || LOOPBACK.test(m[2]);
}

/** codeModules reads a code from the service, { size, rows } with each
    row in hex, its first module the top bit: dark(x, y), or null where it
    isn't one Dens makes. */
export function codeModules(code) {
    const size = code?.size;
    if (!Number.isInteger(size) || size < 21 || size > 57 || (size - 17) % 4 !== 0) return null;
    const width = 2 * Math.ceil(size / 8);
    if (!Array.isArray(code.rows) || code.rows.length !== size) return null;
    if (!code.rows.every((row) => typeof row === 'string' && row.length === width && /^[0-9a-f]+$/.test(row))) return null;
    const bytes = code.rows.map((row) => Array.from({ length: width / 2 }, (_, i) => parseInt(row.slice(2 * i, 2 * i + 2), 16)));
    return (x, y) => x >= 0 && y >= 0 && x < size && y < size && ((bytes[y][x >> 3] >> (7 - (x & 7))) & 1) === 1;
}

/** codePath is a code's dark modules as an SVG path, a rectangle for each
    run along a row, a module a unit; '' where it isn't one Dens makes. */
export function codePath(code) {
    const dark = codeModules(code);
    if (!dark) return '';
    let d = '';
    for (let y = 0; y < code.size; y++) {
        for (let x = 0; x < code.size; x++) {
            if (!dark(x, y)) continue;
            let run = 1;
            while (dark(x + run, y)) run++;
            d += `M${x} ${y}h${run}v1h-${run}z`;
            x += run;
        }
    }
    return d;
}
