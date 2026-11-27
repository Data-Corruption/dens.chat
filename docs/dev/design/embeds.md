# Players and cards

Part of [Dens's design](../design.md): link previews, YouTube's players and Steam's cards.

## Link previews

YouTube's players and Steam's cards each show their site the member's IP address, so the browser keeps whether each shows, in the settings' Chat section under Links.

- A browser new to Dens, its storage holding nothing of Dens's as the page loads, asks as it opens Dens: on the first run's local password, or, where that's set, on a screen of its own before any page, a den's too, titled This browser is paired.
- One switch, Link previews, starts on, with a line saying YouTube and Steam see the member's IP address; turned off, it opens into a switch for each, both off. The browser keeps the answer, 1 or 0 for each (`DENS_YOUTUBE`, `DENS_STEAM`).
- A browser that had opened Dens before keeps what it had, nothing being off. `dens open` pairs a browser each time it opens the page, so the question rests on the browser's storage rather than on its pairing.

## YouTube players

A member can play a linked YouTube video where it's posted, in the message list, rather than in a new tab, where their browser shows link previews (see [Link previews](#link-previews)), since it means their Dens and their browser contact YouTube.

- With players on, a YouTube link in a message gets a cover under the message, after its files: the video's picture with its title and channel over it, as links to the video and the channel on YouTube, and a play button. Only links in the forms the den writes count (`https://www.youtube.com/watch?v=…`, with `&t=` and a start time if there is one, and a Short's `https://www.youtube.com/shorts/…`), each video once, and at most three per message. A link in a spoiler gets none, and reply quotes, bios and channel descriptions get none either.
- The cover takes a 16:9 box before anything in it loads, and the player takes the same box, so the message list never moves. The box is the member's choice, Appearance's *YouTube players*: Small, Medium, Large or Largest, 400, 640, 960 or 1,280 pixels wide, Small until they choose, which the browser keeps (`DENS_PLAYER_SIZE`); it never runs past the pane, and the phone layout keeps Small. A Short's box is 9:16, as tall as another's is wide, up to 720 pixels, with the Short's own picture. A Dens from before v1.11.0 shows a Short's link as a link, with no cover, and a den from before it writes a Short as a video, which plays landscape.
- Clicking the cover swaps in YouTube's player from `www.youtube-nocookie.com`, which plays at once, from the link's start time. The title and the channel's name, which lie over the cover's play button and take the pointer only where they're words, open the video, at the link's start time, and the channel on YouTube in a new tab, as a message's links do, while a click anywhere else on the cover plays it.
- One video plays at a time across YouTube players and Steam trailers: starting another puts a YouTube video back behind its cover and stops a Steam trailer back to its picture. A playing video carries on while its message is scrolled out of view, so music and picture-in-picture keep going, and stops once its message leaves the part of the list that's loaded, or the member opens another channel.
- A video whose owner turned off embedding says it plays only on YouTube, with its link, instead of offering to play.

### The cover

The member's local service fetches the title, channel and picture once a cover comes on screen, and the page shows them from its own origin, so its CSP lets in no image or connection from YouTube.

- The service asks YouTube's oEmbed endpoint for the title and channel, and `i.ytimg.com` for the picture, at addresses it builds from the video's ID: 11 of `A-Z`, `a-z`, `0-9`, `-` and `_`. The picture is `hqdefault.jpg`, 480 by 360; for a player past Small `maxresdefault.jpg`, 1280 by 720, or the usual one where YouTube has none; or for a Short `oar2.jpg`, the portrait picture YouTube shows it with, 1080 by 1920; where YouTube has none, it's the usual one, a frame of the Short between blurred bands, which the portrait box crops to the frame. It follows no redirects, reads at most 64 KiB of an answer, 256 KiB of a picture and 1 MiB of a larger one or a Short's, and gives up after 10 seconds.
- The title and channel are cleaned as display names are, and shown as text. The channel's address, oEmbed's `author_url`, is kept beside its name only as an https address on `www.youtube.com`, written out again from its parts, and the page links the name to no other address either; without one, the name stays text. Nothing else in oEmbed's answer is used, the ready-made player it offers least of all. The picture must be a JPEG, which `internal/media` reads and strips, and the page gets it with the headers of a member's file, so the browser doesn't keep it.
- The service sends no cookies, referrer or user agent. It holds what it fetched in memory for an hour, and a failure for a minute, up to 16 MiB in all; asks once for a video that several covers want at the same time; and never logs a video's ID, which comes from a message.

### Who learns what

The setting says so, in fewer words:

- YouTube sees the member's IP address for each video whose cover comes on screen, in channels and in DMs, and which videos they play. `youtube-nocookie.com` holds YouTube's cookies back until a video plays; after that, the browser's own rules on third-party cookies decide whether YouTube can tie the play to a Google account.
- Several members of a den looking up one video at about the same time could let YouTube guess they know each other.
- In a DM, a cover tells YouTube which video the member was sent. The den still can't read it.
- Nobody but YouTube learns anything. Its addresses are fixed, so a member can't point anyone's Dens at a server of their own, which is why other links get no previews. With players off, links show as they always have, and nothing on the page contacts YouTube.

### The player's frame

YouTube's player is a frame from another origin, so the browser keeps it from the page, the page's storage and the local API.

- It's sandboxed to scripts, its own origin and popups: it can't navigate the page, download files or open dialogs. A popup, such as YouTube's "Watch on YouTube", opens in a new tab that can't reach the page, since the page's `Cross-Origin-Opener-Policy: same-origin` cuts it off.
- It's allowed autoplay, encrypted media, picture-in-picture and full screen, and nothing else. The page's `Permissions-Policy` keeps the microphone and camera from it whatever it asks.
- The page builds the frame's address from the ID and start time it checked itself, since a hostile den or sender could skip the den's rewrite. Its CSP takes frames only from `https://www.youtube-nocookie.com`.
- The page listens for no messages from the frame, and doesn't use YouTube's player API, which would need it to.

### The page's address

YouTube's player needs a `Referer` naming the page that embeds it, and won't play for a page at an IP address: `127.0.0.1`, `[::1]` and LAN addresses all get "This video is unavailable", while host names, `localhost` included, play. So the page lives at `localhost` (see [Pairing the browser](../design.md#pairing-the-browser)). The frame sends YouTube the page's origin and nothing more (`strict-origin-when-cross-origin`); everything else the page loads sends no referrer at all.

## Steam's cards

A member can see a linked game on Steam as a card under its message, where their browser shows link previews (see [Link previews](#link-previews)), since it means their Dens contacts Steam.

- With cards on, a link to an app on Steam gets a card under the message, after its videos: its store page, `https://store.steampowered.com/app/<ID>…`, its age check, `…/agecheck/app/<ID>…`, or its community hub, `https://steamcommunity.com/app/<ID>…`, by the app's ID, digits with no leading zero up to 4,294,967,295, with anything after it. The den doesn't shorten Steam's links, so the page reads them as members wrote them. Each app gets one card, at most three to a message, and a link in code or a spoiler gets none; reply quotes, bios and channel descriptions get none either. Bundles, packages and `steam://` links get no card.
- A card's size is its own choice in Appearance, *Steam cards*: Small, Medium, Large or Largest, 400, 640, 960 or 1,280 pixels wide, Small until the member chooses, which the browser keeps (`DENS_STEAM_SIZE`); it never runs past the pane, and the phone layout keeps Small. Medium lays the 16:9 picture or trailer beside the facts (56.25% of the card's width), where both share a height; Small, Large and Largest stack the full-width 16:9 picture or trailer above the facts, with Large and Largest using a compact wide bar below so larger cards give trailers full width without empty space. Every row keeps a fixed height before anything loads, so the message list never moves across loading, screenshots or trailer playback. The card holds the app's header picture, whose arrows step through its first screenshots, up to four, each fetched as it's shown; its primary trailer on click from the header slide; its name, linking to its store page; its developer and top three tags; its description; its reviews, named as Steam names their score, with a mark green past Mixed, amber at it and red under it, their share positive and their count; how many play it now; the systems it runs on, Linux among them, and Steam Deck's rating; and its price where the member is, with a sale's cut, the price before and its end, Free to play, or nothing while it's coming, beside when it came out or that it's coming.
- Clicking the play button on a card's header slide plays the app's primary trailer in the card's picture box with the same player controls and saved volume as uploaded videos, sharing the one-at-a-time playback lock with YouTube players and other Steam cards. Stepping to screenshots while a trailer plays keeps it playing behind a corner pill that jumps back to the trailer or stops it. The page prefers `h264` (`avc1` + `mp4a.40.2`) and falls back to `av1` when rejected or absent, picking up to 480p on Small and the phone layout and up to 720p on Medium and larger. It plays through Media Source Extensions with two `SourceBuffer`s (video and separate AAC audio) attached via `URL.createObjectURL(MediaSource)`, which requires `media-src 'self' blob:` in the page's CSP, streaming 3-second chunks ahead of playback and revoking the object URL and aborting fetches on stop or unmount.
- An app Steam marks for adults only (its content descriptors 3 and 4) keeps its pictures and trailer play button covered, asking for none, until the member shows them; mature screenshots are never fetched, while a trailer falls back from all-ages highlights to the app's first highlight only when the card is covered.
- A game Steam doesn't have says so in its card, and one Steam didn't answer for says Steam couldn't be reached, its name still linking to the store.

### The card's facts

The member's local service fetches them once a card comes on screen, and the page shows them from its own origin, so its CSP lets in nothing from Steam.

- It asks Steam's store for the app (`IStoreBrowseService/GetItems` on `api.steampowered.com`, with `include_trailers: true`, for the US store, since it answers nothing without a country), its price where the member is (`api/appdetails` on `store.steampowered.com`, which prices by the address asking), how many play it now (`ISteamUserStats/GetNumberOfCurrentPlayers`), and the tags' names (`IStoreService/GetTagList`, kept a day). A sale's end shows only where the US store's cut is the member's. A price, a count of players or tags that doesn't come leaves the card without them.
- Pictures come only from `https://shared.akamai.steamstatic.com/store_item_assets/`, at the path Steam's answer gives, which must start with `steam/apps/<ID>/`, take one folder below it at most, of letters, digits, `_`, `.` and `-` and never `..`, end in `.jpg`, and carry no query but `t=` and digits. The header is Steam's larger one where it has one; a screenshot is its 600 by 338 thumbnail.
- Trailers come only from `https://video.akamai.steamstatic.com/store_trailers/`, fetched only when the member clicks play, never when fetching a card. The primary `all_ages: true` highlight in `trailers.highlights[]` is preferred; if none exists on an adult-only app, the first valid trailer in `trailers.highlights[]` or `trailers.other_trailers[]` is used (kept behind the adult cover). Its `dash_h264` and `dash_av1` `cdn_path` must match `^<appID>/[0-9]{1,12}/[0-9a-f]{40}/[0-9]{1,12}/dash_(h264|av1)\.mpd$`. The service parses the DASH MPD (capped at 64 KiB, rejecting XML directives, `BaseURL`, query strings, traversal and non-ISO-BMFF templates, and ignoring `image/` adaptation sets) into clean JSON track descriptors with no raw CDN paths. Segments (`/api/steam/{app}/trailer/{encoding}/{rep}/init` and `/…/{seg}`) are resolved strictly from validated `dash_(h264|av1)/init-stream<rep>.m4s` and `chunk-stream<rep>-<5-digit-num>.m4s` templates, capped at 4 MiB, sniffed for ISO-BMFF (`ftyp` for init, `styp` or `moof` for media chunks), and served with `Cache-Control: no-store`, `Content-Security-Policy: default-src 'none'; sandbox` and `X-Content-Type-Options: nosniff`.
- It follows no redirects, reads at most 1 MiB of the store's answer, 256 KiB of the tag list, 16 KiB of a price, 4 KiB of a count, 1 MiB of a picture (at most 1,920 pixels a side), 64 KiB of a trailer manifest and 4 MiB of a trailer segment, and gives up after 10 seconds, asking at most 8 times a second. It keeps a card's facts, its price and its pictures an hour, its players 10 minutes, an app Steam lacks an hour, and a failure a minute, in memory within 16 MiB; trailer manifests and segments sit in a separate 32 MiB memory cache for 10 minutes so playback never evicts cards or pictures.
- Names, the developer, the description, tags, a release's note and formatted prices are cleaned as display names are and cut to length, and shown as text; a number out of its range is dropped. The page names reviews from their score alone, and links only to the store page it builds from the ID it checked. Pictures must be JPEGs, which `internal/media` reads and strips, and the page gets them with the headers of a member's file, so the browser doesn't keep them.

### What Steam sees

Steam sees the member's IP address for each game whose card comes on screen, in channels and in DMs, which pictures they step to, and which trailers they play. Nobody else learns anything: its addresses are fixed, so a member can't point anyone's Dens at a server of their own. With cards off, links show as they always have, and nothing on the page contacts Steam.
