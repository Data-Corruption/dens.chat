// YouTube's players (M4.1). With them on, a message's YouTube links get
// covers under it: each video's picture, title and channel, which the local
// service fetched, and a play button that swaps in YouTube's own player,
// in the same box. One plays at a time.
//
// The player is a frame from youtube-nocookie.com, sandboxed: it can't
// navigate the page, its popups can't reach it, and the page listens to
// nothing it says. Its address is built here, from a video ID and a start
// time the page checked itself.

import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api } from './api.js';
import { onYouTubePlayers, youTubePlayers } from './links.js';
import { videosIn } from './markdown.jsx';

const SANDBOX = 'allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox';
const ALLOW = 'autoplay; encrypted-media; picture-in-picture; fullscreen';

// embedURL is the address of YouTube's player for a video, which plays at
// once: the member clicked to play it.
export function embedURL({ id, start }) {
    return `https://www.youtube-nocookie.com/embed/${id}?autoplay=1${start > 0 ? `&start=${start}` : ''}`;
}

export function watchURL({ id, start }) {
    return `https://www.youtube.com/watch?v=${id}${start > 0 ? `&t=${start}` : ''}`;
}

// playing is the player whose video plays: an object only that player
// holds, so a cover for the same video elsewhere in the list stays a cover.
let playing = null;
const playingListeners = new Set();

function play(player) {
    playing = player;
    playingListeners.forEach((fn) => fn());
}

// Videos shows the covers of a message's YouTube links, after its files,
// while this browser has players on.
export function Videos({ text }) {
    const [on, setOn] = useState(youTubePlayers);
    useEffect(() => onYouTubePlayers(setOn), []);
    const videos = useMemo(() => (on ? videosIn(text) : []), [on, text]);
    if (!videos.length) return null;
    return (
        <div class="mt-1 flex flex-col gap-1">
            {videos.map((v) => <Player key={v.id} video={v} />)}
        </div>
    );
}

// Player is a video's cover, which becomes YouTube's player when clicked.
// Both take one 16:9 box, sized before anything in it loads, so the message
// list never moves. The cover asks about its video once it comes on screen,
// since YouTube hears of each video asked about.
function Player({ video }) {
    const self = useRef(null);
    if (!self.current) self.current = {};
    const box = useRef(null);
    const [, rerender] = useState(0);
    const [seen, setSeen] = useState(false);
    const [about, setAbout] = useState(null);

    useEffect(() => {
        const changed = () => rerender((n) => n + 1);
        playingListeners.add(changed);
        return () => {
            playingListeners.delete(changed);
            // A video stops with its message, when that leaves the list or
            // its channel closes, and doesn't start again when it's back.
            if (playing === self.current) playing = null;
        };
    }, []);

    useEffect(() => {
        const observer = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting)) {
                setSeen(true);
                observer.disconnect();
            }
        });
        observer.observe(box.current);
        return () => observer.disconnect();
    }, []);

    useEffect(() => {
        if (!seen) return undefined;
        let live = true;
        api.get(`/api/youtube/${video.id}`)
            .then((v) => live && setAbout({ ...v, found: true }))
            .catch((e) => live && setAbout(e.status === 404 ? { missing: true } : { failed: true }));
        return () => {
            live = false;
        };
    }, [seen, video.id]);

    let inside;
    if (playing === self.current) {
        // The frame's referrer is the page's origin, which YouTube's player
        // needs; the page itself sends none.
        inside = (
            <iframe
                class="absolute inset-0 size-full"
                title={about?.title || 'YouTube video'}
                sandbox={SANDBOX}
                allow={ALLOW}
                referrerpolicy="strict-origin-when-cross-origin"
                src={embedURL(video)}
            />
        );
    } else if (about?.missing) {
        inside = <Notice>YouTube doesn't have this video.</Notice>;
    } else if (about?.found && !about.playable) {
        inside = (
            <>
                <Picture id={video.id} dim />
                <Notice>
                    This video plays only on YouTube.
                    <a href={watchURL(video)} target="_blank" rel="noopener noreferrer nofollow" class="link font-medium text-white">
                        Watch it there
                    </a>
                </Notice>
            </>
        );
    } else {
        inside = (
            <button type="button" class="group absolute inset-0 size-full cursor-pointer" onClick={() => play(self.current)} aria-label={`Play ${about?.title || 'the YouTube video'}`}>
                {about?.found && <Picture id={video.id} />}
                {about?.title && (
                    <span class="absolute inset-x-0 top-0 flex flex-col bg-linear-to-b from-black/80 via-black/50 to-transparent px-3 pb-6 pt-2 text-left text-white">
                        <span class="truncate text-sm font-semibold">{about.title}</span>
                        {about.channel && <span class="truncate text-xs text-white/80">{about.channel}</span>}
                    </span>
                )}
                <span class="absolute left-1/2 top-1/2 flex h-12 w-17 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-2xl bg-black/70 transition-colors group-hover:bg-[#f00] group-focus-visible:bg-[#f00]">
                    <svg viewBox="0 0 24 24" class="size-7 fill-white" aria-hidden="true">
                        <path d="M8 5.5v13l11-6.5z" />
                    </svg>
                </span>
            </button>
        );
    }
    return (
        <div ref={box} data-video={video.id} class="relative aspect-video w-[400px] max-w-full overflow-hidden rounded bg-black">
            {inside}
        </div>
    );
}

function Picture({ id, dim }) {
    return (
        <img
            src={`/api/youtube/${id}/thumb`}
            alt=""
            class={`absolute inset-0 size-full object-cover ${dim ? 'opacity-40' : ''}`}
            onError={(e) => {
                e.currentTarget.hidden = true;
            }}
        />
    );
}

function Notice({ children }) {
    return <div class="absolute inset-0 flex flex-col items-center justify-center gap-1 p-4 text-center text-sm text-white/90">{children}</div>;
}
