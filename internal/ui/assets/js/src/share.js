// Screen share's rules (M4.2), apart from the page's call so the tests can
// check them: what the page asks the browser to capture and send, within
// the den's limits and the member's choices; which sections of the den's
// offers carry the page's screen and its sound; who shares and watches in
// a call; and what a refusal says.

// A member's choices, which the browser keeps: the share's size, as the
// height of a 16:9 picture, its frame rate, whether it suits text or
// motion, and whether it carries sound where the browser can share it.
export const SHARE_DEFAULTS = { height: 1080, fps: 30, hint: 'detail', sound: true };
export const HEIGHTS = [720, 1080, 1440, 2160];
export const RATES = [15, 30, 60];
const HINTS = ['detail', 'motion'];

// MAX_WATCHING is how many shares a member watches at once.
export const MAX_WATCHING = 4;

// readShare reads the choices the browser kept, falling back to the
// defaults for anything that isn't one.
export function readShare(text) {
    let kept;
    try {
        kept = JSON.parse(text || '{}');
    } catch {
        kept = {};
    }
    if (!kept || typeof kept !== 'object' || Array.isArray(kept)) kept = {};
    return {
        height: HEIGHTS.includes(kept.height) ? kept.height : SHARE_DEFAULTS.height,
        fps: RATES.includes(kept.fps) ? kept.fps : SHARE_DEFAULTS.fps,
        hint: HINTS.includes(kept.hint) ? kept.hint : SHARE_DEFAULTS.hint,
        sound: typeof kept.sound === 'boolean' ? kept.sound : SHARE_DEFAULTS.sound,
    };
}

// shareQuality is what a share goes at: the member's choices, each held to
// the den's limits, and the den's bitrate.
export function shareQuality(limits, prefs) {
    return {
        height: Math.min(prefs.height, limits.share_height),
        fps: Math.min(prefs.fps, limits.share_fps),
        bitrate: limits.share_bitrate,
    };
}

// fitPixels scales a surface of width × height down, keeping its shape,
// until it has no more pixels than a 16:9 picture of height cap: 1080
// allows as many as 1920 × 1080, whether the surface is ultrawide or
// standing on end. It never scales up. scale is how much smaller each side
// gets, which a sender's scaleResolutionDownBy takes.
export function fitPixels(width, height, cap) {
    const most = Math.round((cap * 16) / 9) * cap;
    if (!(width > 0 && height > 0) || width * height <= most) return { width, height, scale: 1 };
    const scale = Math.sqrt((width * height) / most);
    // Encoders take even sizes.
    const even = (n) => Math.max(2, Math.floor(n / 2) * 2);
    return { width: even(width / scale), height: even(height / scale), scale };
}

// displayOptions is what getDisplayMedia is asked for: the frame rate, and
// sound if the member wants it, without the call's own voices, which
// restrictOwnAudio keeps out of a system's sound, and without processing
// meant for a microphone. The Dens tab stays out of the picker, since
// sharing it would show the page inside itself.
export function displayOptions(quality, sound) {
    return {
        video: { frameRate: { ideal: quality.fps, max: quality.fps } },
        audio: sound ? { echoCancellation: false, noiseSuppression: false, autoGainControl: false, restrictOwnAudio: true } : false,
        selfBrowserSurface: 'exclude',
        surfaceSwitching: 'include',
        systemAudio: sound ? 'include' : 'exclude',
        windowAudio: sound ? 'window' : 'exclude',
        monitorTypeSurfaces: 'include',
    };
}

// receivingSections finds the sections of an offer the den receives on
// besides the microphone's, which is always the first: the video section
// for the member's screen, and the second audio section for its sound.
// Either is null until the den adds it, at the member's first share.
export function receivingSections(sdp) {
    const found = { screen: null, sound: null };
    let audio = 0;
    for (const section of String(sdp).split(/\r?\nm=/).slice(1)) {
        const kind = section.slice(0, section.indexOf(' '));
        const mid = /\r?\na=mid:([^\r\n]+)/.exec(section)?.[1];
        if (!mid || !/\r?\na=recvonly(\r|\n|$)/.test(section)) continue;
        if (kind === 'video' && found.screen === null) found.screen = mid;
        if (kind === 'audio' && ++audio === 2) found.sound = mid;
    }
    return found;
}

// screenOwner says whose share a stream is: the den names a share's stream
// for its member, as "screen-<member ID>". It's '' for any other stream.
export function screenOwner(streamID) {
    const m = /^screen-([1-9][0-9]{0,18})$/.exec(String(streamID));
    return m ? m[1] : '';
}

// sharersIn lists who shares in a call, and watchersOf who watches a
// member's share.
export function sharersIn(call) {
    return (call?.members || []).filter((m) => m.sharing).map((m) => m.id);
}

export function watchersOf(call, member) {
    return (call?.members || []).filter((m) => (m.watching || []).includes(member)).map((m) => m.id);
}

// sharingAcross counts who shares in a den's calls the page can see.
export function sharingAcross(calls) {
    return (calls || []).reduce((n, c) => n + sharersIn(c).length, 0);
}

// shareBlocked says why the member can't start a share now, before they
// pick anything, or '' when they can: the den allows none, or as many
// share as it allows. The den checks again all the same.
export function shareBlocked(limits, calls) {
    if (!limits) return '';
    if (limits.shares === 0) return "This den doesn't allow screen sharing.";
    const n = sharingAcross(calls);
    if (n >= limits.shares) return limits.shares === 1 ? 'Someone is sharing already, and the den allows one share at a time.' : `${n} people are sharing, as many as the den allows at once.`;
    return '';
}

// refusalText says why the den refused a share or a watch.
export function refusalText(what, reason, limits) {
    if (what === 'share') {
        switch (reason) {
            case 'off':
                return "This den doesn't allow screen sharing.";
            case 'full':
                return limits?.shares === 1 ? 'Someone else started sharing first; the den allows one share at a time.' : 'As many people are sharing as the den allows.';
            case 'staff_muted':
                return "Staff muted you, so you can't share your screen until they lift it.";
            case 'rate_limited':
                return "You're doing that too fast. Wait a moment and try again.";
        }
        return "The den couldn't start your share.";
    }
    switch (reason) {
        case 'full':
            return `That share has as many viewers as the den allows${limits ? ` (${limits.share_viewers})` : ''}, or you're watching ${MAX_WATCHING} already.`;
        case 'not_sharing':
            return 'That share has ended.';
        case 'rate_limited':
            return "You're doing that too fast. Wait a moment and try again.";
    }
    return "The den couldn't show you that share.";
}

// shareCost is what a den's limits cost its upload at most, in bits a
// second: video, every share at its bitrate to every viewer it may have,
// and voice, a full call at bitrate while two members talk, with about a
// fifth more for the packets' headers.
export function shareCost(limits, voiceBitrate) {
    const video = limits.shares * limits.share_viewers * limits.share_bitrate * 1.05;
    const voice = 2 * Math.max(0, limits.members - 1) * voiceBitrate * 1.2;
    return { video, voice };
}

// mbps writes bits a second as megabits, to one place under 10.
export function mbps(bits) {
    const n = bits / 1e6;
    return n < 10 ? `${Math.round(n * 10) / 10} Mbps` : `${Math.round(n)} Mbps`;
}
