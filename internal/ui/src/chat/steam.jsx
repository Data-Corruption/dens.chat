// Steam's cards. With them on, a message's links to games on Steam get
// cards under it: the game's picture, with its first screenshots a step
// away and its primary trailer on click, its name linking to its store
// page, who made it, its tags and a line on it; its reviews and how many
// play it now; the systems it runs on, Steam Deck's rating among them; and
// its price where the member is, with a sale's end. The member's Dens
// fetches all of it from Steam once a card comes on screen, and the page
// shows it from its own origin. A card takes its whole box before anything
// in it loads, so the message list never moves, and a game Steam marks for
// adults only keeps its pictures and trailer covered until the member shows
// them.

import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../core/api.js';
import { MenuIcon, PlayIcon } from '../ui/icons.jsx';
import { onSteamCards, steamCards } from './links.js';
import { gamesIn } from './markdown.jsx';
import { claimPlay, isPlaying, onPlayChange, releasePlay } from './playback.js';
import { STEAM, steamCardClass, steamMediaClass, useShownSize } from '../appearance/sizes.js';
import { startTrailer } from './trailer.js';
import { useCoarse } from '../core/touch.js';

// REVIEWS are Steam's names for its reviews' scores, from 1 to 9.
const REVIEWS = ['', 'Overwhelmingly Negative', 'Very Negative', 'Negative', 'Mostly Negative', 'Mixed', 'Mostly Positive', 'Positive',
    'Very Positive', 'Overwhelmingly Positive'];

/** reviewLook is how a card shows reviews: their name, as Steam's, and
    the color of their mark, green past Mixed, amber at it, red under it;
    null for none to show */
export function reviewLook(reviews) {
    const score = reviews?.score;
    if (!Number.isInteger(score) || score < 1 || score > 9) return null;
    return { name: REVIEWS[score], tone: score > 5 ? 'text-success' : score === 5 ? 'text-warning' : 'text-error' };
}

/** count is a number as a card writes one: in full up to 99,999, and
    shortened past it, as 148K */
export function count(n, locale) {
    return new Intl.NumberFormat(locale, n > 99999 ? { notation: 'compact', maximumFractionDigits: 1 } : {}).format(n);
}

/** systems lists what a card says a game runs on */
export function systems(card) {
    const p = card.platforms || {};
    return [p.windows && 'Windows', p.mac && 'macOS', p.linux && 'Linux'].filter(Boolean);
}

const DECK = { verified: ['Verified', 'text-success'], playable: ['Playable', 'text-warning'], unsupported: ['Unsupported', 'text-base-content/60'] };

/** deckLook is Steam Deck's rating as a card shows it, or null */
export function deckLook(deck) {
    const d = DECK[deck];
    return d ? { name: d[0], tone: d[1] } : null;
}

/** when is the date a card shows a game by, beside its price: a sale's
    end while one runs, or else when it came out, or that it's coming */
export function when(card, now = Date.now(), locale = undefined) {
    const day = (s) => {
        const d = new Date(s * 1000);
        const opts = { month: 'short', day: 'numeric' };
        if (d.getFullYear() !== new Date(now).getFullYear()) opts.year = 'numeric';
        return d.toLocaleDateString(locale, opts);
    };
    if (card.price?.discount > 0 && card.price.ends * 1000 > now) return `Sale ends ${day(card.price.ends)}`;
    if (card.coming_soon) return card.release_note || (card.released ? `Coming ${day(card.released)}` : 'Coming soon');
    return card.released ? `Released ${day(card.released)}` : '';
}

// storeURL is the store's page of an app, built from the ID the page
// checked.
const storeURL = (app) => `https://store.steampowered.com/app/${app}/`;

// Games shows the cards of a message's links to Steam, after its videos,
// while this browser has them on.
export function Games({ text }) {
    const [on, setOn] = useState(steamCards);
    useEffect(() => onSteamCards(setOn), []);
    const apps = on ? gamesIn(text) : [];
    if (!apps.length) return null;
    return (
        <div class="mt-1 flex flex-col gap-1">
            {apps.map((app) => <Card key={app} app={app} />)}
        </div>
    );
}

// Card is a game's card. It asks about its game once it comes on screen,
// since Steam hears of each game asked about, and its rows keep their
// heights whatever comes, so it never changes size.
function Card({ app }) {
    const size = useShownSize(STEAM);
    const box = useRef(null);
    const [seen, setSeen] = useState(false);
    const [about, setAbout] = useState(null);
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
        api.get(`/api/steam/${app}`)
            .then((v) => live && setAbout({ ...v, found: true }))
            .catch((e) => live && setAbout(e.status === 404 ? { missing: true } : { failed: true }));
        return () => {
            live = false;
        };
    }, [seen, app]);
    const card = about?.found ? about : null;
    const look = card && reviewLook(card.reviews);
    const deck = card && deckLook(card.deck);
    const on = card ? systems(card) : [];
    const date = card ? when(card) : '';
    const link = 'link link-hover';
    return (
        <div ref={box} data-steam={app} data-steam-size={size}
            data-state={card ? 'found' : about?.missing ? 'missing' : about?.failed ? 'failed' : 'loading'}
            class={`flex ${steamCardClass(size)} max-w-full overflow-hidden rounded-box border border-base-300 bg-base-200 text-sm`}>
            <Pictures app={app} card={card} size={size}
                notice={about?.missing ? "Steam doesn't have this game." : about?.failed ? "Steam couldn't be reached." : ''} />
            <div class="flex min-w-0 flex-1 flex-col gap-1.5 p-3">
                <div class="flex h-6 items-center gap-2">
                    <a href={storeURL(app)} target="_blank" rel="noopener noreferrer nofollow" class={`${link} min-w-0 flex-1 truncate text-base font-semibold`}
                        data-steam-name>
                        {card?.name || (about ? 'Open it on Steam' : '')}
                    </a>
                    <span class="shrink-0 cursor-default select-none text-[10px] font-semibold tracking-widest text-base-content/50">STEAM</span>
                </div>
                <p class="h-4 truncate text-xs text-base-content/70" data-steam-by>{card ? [card.developer, ...(card.tags || [])].filter(Boolean).join(' · ') : ''}</p>
                <p class="line-clamp-2 h-10 text-base-content/85" data-steam-about>{card?.description || ''}</p>
                <div class="flex h-5 items-center gap-3 overflow-hidden whitespace-nowrap text-xs">
                    {look && (
                        <span class="flex min-w-0 items-center gap-1" data-steam-reviews={card.reviews.score}>
                            <span class={look.tone}><MenuIcon name={card.reviews.score >= 5 ? 'thumb' : 'thumbdown'} size="h-3.5 w-3.5" /></span>
                            <span class="truncate">{look.name}</span>
                            <span class="text-base-content/60">{card.reviews.percent}% of {count(card.reviews.count)}</span>
                        </span>
                    )}
                    {card?.players > 0 && (
                        <span class="ml-auto flex shrink-0 items-center gap-1.5" data-steam-players={card.players}>
                            <span class="size-2 rounded-full bg-success" aria-hidden="true"></span>
                            {count(card.players)} playing
                        </span>
                    )}
                </div>
                <div class="flex h-5 items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs text-base-content/70" data-steam-systems>
                    {on.join(' · ')}
                    {deck && (
                        <span class="flex items-center gap-1" data-steam-deck={card.deck}>
                            {on.length > 0 && <span aria-hidden="true">·</span>}
                            Steam Deck
                            <span class={`flex items-center gap-0.5 ${deck.tone}`}>
                                <MenuIcon name={card.deck === 'verified' ? 'read' : 'info'} size="h-3.5 w-3.5" />
                                <span class="text-base-content/70">{deck.name}</span>
                            </span>
                        </span>
                    )}
                </div>
                <div class="mt-auto flex h-7 items-center gap-2 whitespace-nowrap">
                    <Price card={card} />
                    <span class="ml-auto truncate text-xs text-base-content/60" data-steam-when>{date}</span>
                </div>
            </div>
        </div>
    );
}

// Price is what a game costs where the member is: a sale's cut, its
// price before and after; free to play; or nothing, while it's coming.
function Price({ card }) {
    if (!card) return null;
    if (card.free) return <span class="font-semibold" data-steam-price="free">Free to play</span>;
    const p = card.price;
    if (!p?.final) return null;
    return (
        <span class="flex items-center gap-2" data-steam-price={p.final}>
            {p.discount > 0 && <span class="rounded bg-success px-1.5 py-0.5 text-xs font-bold text-success-content">-{p.discount}%</span>}
            {p.discount > 0 && p.original && <span class="text-xs text-base-content/50 line-through">{p.original}</span>}
            <span class="font-semibold">{p.final}</span>
        </span>
    );
}

// Pictures is a card's picture, Steam's header for the game, the
// screenshots its arrows step through, each fetched once it's shown, and
// its primary trailer on click. A game for adults only shows none until
// the member asks. notice says why a card has nothing to show.
function Pictures({ app, card, size, notice }) {
    const touch = useCoarse();
    const token = useRef(null);
    if (!token.current) token.current = {};
    const videoRef = useRef(null);
    const [at, setAt] = useState(0);
    const [shown, setShown] = useState(false);
    const [broken, setBroken] = useState(-1);
    const [playing, setPlaying] = useState(false);
    const [loading, setLoading] = useState(false);
    const [trailerError, setTrailerError] = useState('');

    useEffect(() => {
        const stop = onPlayChange(() => setPlaying(isPlaying(token.current)));
        return () => {
            stop();
            releasePlay(token.current);
        };
    }, []);

    useEffect(() => {
        if (!playing || !videoRef.current) return undefined;
        setLoading(true);
        setTrailerError('');
        const stop = startTrailer({
            app,
            size,
            video: videoRef.current,
            onReady: () => setLoading(false),
            onError: () => {
                setLoading(false);
                setTrailerError("Trailer couldn't be played.");
                releasePlay(token.current);
            },
        });
        return () => {
            setLoading(false);
            stop();
        };
    }, [playing, app, size]);

    const all = 1 + (card?.shots || 0);
    const src = at === 0 ? `/api/steam/${app}/header` : `/api/steam/${app}/shots/${at}`;
    const covered = card?.adult && !shown;
    const step = (by) => {
        releasePlay(token.current);
        setTrailerError('');
        setAt((i) => (i + by + all) % all);
    };
    const play = () => {
        setTrailerError('');
        claimPlay(token.current);
    };
    const arrow = `absolute top-1/2 flex size-8 -translate-y-1/2 items-center justify-center rounded-full bg-black/60 text-white hover:bg-black/80 focus-visible:opacity-100 ${touch ? '' : 'opacity-0 group-hover/pics:opacity-100'}`;
    return (
        <div class={`group/pics relative ${steamMediaClass(size)} shrink-0 overflow-hidden bg-base-300`} data-steam-picture={at}>
            {playing && !covered ? (
                <>
                    <video ref={videoRef} controls playsinline class="size-full bg-black object-contain" data-steam-video />
                    {loading && (
                        <span class="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/30">
                            <span class="loading loading-spinner loading-md text-white"></span>
                        </span>
                    )}
                    <button type="button"
                        class="absolute right-2 top-2 flex size-8 cursor-pointer items-center justify-center rounded-full bg-black/70 text-sm text-white hover:bg-black/90"
                        aria-label="Close trailer" data-steam-close onClick={() => releasePlay(token.current)}>
                        ✕
                    </button>
                </>
            ) : (
                <>
                    {card && !covered && broken !== at && (
                        <img key={src} src={src} alt="" class="size-full object-cover" onError={() => setBroken(at)} />
                    )}
                    {covered && (
                        <button type="button" class="absolute inset-0 flex flex-col items-center justify-center gap-1 text-sm" data-steam-cover onClick={() => setShown(true)}>
                            <span class="font-semibold">For adults only</span>
                            <span class="text-xs text-base-content/70">Show the pictures</span>
                        </button>
                    )}
                    {notice && <span class="absolute inset-0 flex items-center justify-center p-4 text-center text-sm text-base-content/70">{notice}</span>}
                    {card && !covered && card.trailer && (
                        <button type="button"
                            class="absolute left-1/2 top-1/2 flex size-12 -translate-x-1/2 -translate-y-1/2 cursor-pointer items-center justify-center rounded-full bg-black/70 text-white transition-colors hover:bg-primary focus-visible:bg-primary"
                            aria-label={`Play ${card.name || 'game'} trailer`} data-steam-play onClick={play}>
                            <PlayIcon size="h-5 w-5" />
                        </button>
                    )}
                    {trailerError && !covered && (
                        <span role="status" data-steam-trailer-error
                            class="absolute inset-x-2 bottom-7 rounded bg-black/80 px-2 py-1 text-center text-xs text-white">
                            {trailerError}
                        </span>
                    )}
                    {card && !covered && all > 1 && (
                        <>
                            <button type="button" class={`${arrow} left-2`} aria-label="Previous picture" onClick={() => step(-1)}>
                                <MenuIcon name="back" size="h-4 w-4" />
                            </button>
                            <button type="button" class={`${arrow} right-2`} aria-label="Next picture" onClick={() => step(1)}>
                                <MenuIcon name="onward" size="h-4 w-4" />
                            </button>
                            <span class="absolute bottom-2 left-1/2 flex -translate-x-1/2 gap-1" aria-hidden="true">
                                {Array.from({ length: all }, (_, i) => (
                                    <span key={i} class={`size-1.5 rounded-full ${i === at ? 'bg-white' : 'bg-white/50'}`}></span>
                                ))}
                            </span>
                        </>
                    )}
                </>
            )}
        </div>
    );
}
