// The owner's edits to a den in its Manage dialog (M8.3): its name and
// address, its upload limits, how long it keeps messages, and its limits
// for calls. They gather in one draft, kept as the fields' text, and go to
// the den as one update of what changed.

import { HEIGHTS } from './share.js';

const MB = 1 << 20;
const GB = 1 << 30;

// MAX_RETENTION is the longest a den keeps messages, in days.
export const MAX_RETENTION = 3650;

// draftOf is a draft of a den's settings as they are, from its status on
// this computer and its state.
export function draftOf(den, view) {
    return {
        name: den.name,
        url: den.url,
        file: String(Math.round(view.limits.file_size / MB)),
        member: String(+(view.limits.member_storage / GB).toFixed(2)),
        den: String(+(view.limits.den_storage / GB).toFixed(2)),
        retain: view.retention > 0,
        days: String(view.retention > 0 ? view.retention : 90),
        ...callFields(view.call_limits),
    };
}

// callFields are call limits as a draft's fields.
export function callFields(l) {
    return {
        members: String(l.members), callers: String(l.callers), shares: String(l.shares), viewers: String(l.share_viewers),
        bitrate: String(l.share_bitrate / 1e6), height: String(l.share_height), fps: String(l.share_fps),
    };
}

// callLimitsOf reads a draft's call limits as the den takes them.
export function callLimitsOf(d) {
    return {
        members: Number(d.members), callers: Number(d.callers), shares: Number(d.shares), share_viewers: Number(d.viewers),
        share_bitrate: Math.round(Number(d.bitrate) * 1e6), share_height: Number(d.height), share_fps: Number(d.fps),
    };
}

const within = (n, min, max) => Number.isFinite(n) && n >= min && n <= max;
const whole = (n, min, max) => Number.isInteger(n) && within(n, min, max);

// changesOf is the update that takes a den from base to draft, of only
// what changed, or a problem to show instead, before the den would refuse
// it.
export function changesOf(draft, base) {
    const body = {};
    const refuse = (problem) => ({ body: null, problem });
    const name = draft.name.trim();
    if (name !== base.name) {
        if (!name) return refuse('The den needs a name.');
        body.name = name;
    }
    const url = draft.url.trim();
    if (url !== base.url) body.url = url;

    if (['file', 'member', 'den'].some((k) => Number(draft[k]) !== Number(base[k]))) {
        const file = Number(draft.file);
        const member = Number(draft.member);
        const den = Number(draft.den);
        if (!whole(file, 1, 1024)) return refuse('Max file size is 1 to 1,024 MB.');
        if (!within(member * GB, file * MB, Infinity)) return refuse('Storage per member is at least the max file size.');
        if (!within(den, member, 1 << 20)) return refuse('Total storage is at least the storage per member.');
        body.limits = { file_size: file * MB, member_storage: Math.round(member * GB), den_storage: Math.round(den * GB) };
    }

    // Days typed while auto-delete is off change nothing.
    if (draft.retain !== base.retain || (draft.retain && Number(draft.days) !== Number(base.days))) {
        const retention = draft.retain ? Number(draft.days) : 0;
        if (draft.retain && !whole(retention, 1, MAX_RETENTION)) return refuse(`Messages are kept 1 to ${MAX_RETENTION.toLocaleString('en-US')} days.`);
        body.retention = retention;
    }

    const calls = callLimitsOf(draft);
    const was = callLimitsOf(base);
    if (Object.keys(calls).some((k) => calls[k] !== was[k])) {
        if (!whole(calls.members, 2, 30)) return refuse('Max people per call is 2 to 30.');
        if (!whole(calls.callers, 2, 100)) return refuse('Max people in calls across the den is 2 to 100.');
        if (!whole(calls.shares, 0, 10)) return refuse('Max shares at once is 1 to 10.');
        if (!whole(calls.share_viewers, 1, 29)) return refuse('Max viewers per share is 1 to 29.');
        if (!within(calls.share_bitrate, 250_000, 50_000_000)) return refuse('Max bitrate is 0.25 to 50 Mbps.');
        if (!HEIGHTS.includes(calls.share_height)) return refuse('Max resolution is 720p, 1080p, 1440p or 4K.');
        if (!whole(calls.share_fps, 5, 60)) return refuse('Max frame rate is 5 to 60 fps.');
        body.call_limits = calls;
    }
    return { body, problem: '' };
}

// changed says whether saving a draft would change anything, or would
// stop at a problem with what changed.
export function changed(draft, base) {
    const { body, problem } = changesOf(draft, base);
    return !!problem || Object.keys(body).length > 0;
}
