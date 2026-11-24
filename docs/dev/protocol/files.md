# Den protocol: files

Uploads, their metadata, attachments, pictures on a profile and the limits on files. The conventions, versions, errors, keys, objects and rate limits every part follows are in [protocol.md](../protocol.md).

## Files

```
file  = {"id", "name", "type", "size", "width"?, "height"?, "animated"?, "duration_ms"?, "thumb"?: {"width", "height"}, "spoiler"?}
image = {"id", "width", "height", "animated"?}
```

A file is an upload: an attachment on a message, or a picture on a profile. `type` is what the den found the file to be from its bytes, never what its name says. `width` and `height` are set for images and videos, as they display, so a client lays out the space one takes before it loads. `duration_ms` is a video's or audio's length when it's known, at most a week. `thumb` is there when the file has a preview. A den sends no `sealed_as`, which a client gives its own page with a DM's file ([Serving](../design/files.md#serving)), and a client refuses a file from a den that carries one.

### Uploading

```
POST /api/uploads      (body: the file's bytes)      201 file
Content-Type: application/octet-stream
Dens-Filename: <the file's name, UTF-8, percent-encoded>
```

- `Content-Length`, when given, lets the den refuse a file over the size limit before reading it. Without it the den reads up to the limit and refuses the file once it passes it.
- The den reads the file's first bytes to decide what it is:
  - JPEG, PNG, GIF and WebP are images. They get their size as they display (after a JPEG's orientation), `animated` when they have more than one frame, and a preview: a JPEG, or a PNG when it has transparency, fitting a 640 × 640 square, never enlarged, upright. An image too large to decode within the den's memory budget, 256 MiB, goes without one and downloads like any other file.
  - Video and audio in MP4 and QuickTime, Matroska and WebM, MP3, AAC, FLAC, Ogg and WAV go through the den's media module (see [Video and audio](#video-and-audio)).
  - HEIF (not AVIF), TIFF (not camera raw), JPEG 2000 and Photoshop photos are refused with `415 unsupported_type`: clients send them as the JPEG or PNG they turn them into.
  - AVIF, JPEG XL, camera raw, and video and audio in other containers (AVI, ASF, FLV, MPEG program and transport streams, AIFF, AMR, MIDI, RealMedia) are refused with `415 unsupported_type`, and a message naming the kind, rather than stored with metadata nobody took out. So are video and audio at a den without the media module.
  - Anything else is a file to download, with the media type the den sniffed, for clients to pick an icon.
- The name is cleaned: control, bidi and zero-width characters are dropped, `/` and `\` become `_`, spaces and dots are trimmed from both ends, and it is cut to 255 bytes without splitting a character. An empty name becomes `file`.
- An upload waits an hour for a message or a profile to use it, and is then deleted. Until it's used, only the member who uploaded it can fetch it.
- Uploads are limited per member (see [Rate limits](../protocol.md#rate-limits)).

### Metadata

Clients take metadata out of images before uploading, so a den never receives it, and the den checks: an image that still has any is refused with `400 invalid_field`. The den re-encodes nothing; a client's smaller copy of a photo is a new image, checked like any other. What an image keeps:

| Kind | Kept | Everything else goes, including |
| --- | --- | --- |
| JPEG | Frame headers, tables, scans, the JFIF header with no thumbnail, the ICC profile, Adobe's color transform (12 bytes), and the orientation | EXIF and its GPS, XMP, IPTC, comments, other application segments, and everything after the image's end, where the Multi-Picture Format stores extra images such as gain maps |
| PNG | `IHDR`, `PLTE`, `IDAT`, `IEND`, `tRNS`, `cHRM`, `gAMA`, `iCCP`, `sBIT`, `sRGB`, `cICP`, `mDCV`, `cLLI`, `bKGD`, `hIST`, `pHYs`, and the animation chunks `acTL`, `fcTL` and `fdAT` | `eXIf` (with its orientation, which browsers don't agree on), text chunks, time stamps, private chunks, anything after `IEND`. An unknown critical chunk refuses the file. |
| GIF | Images, their graphic control extensions, and the `NETSCAPE2.0` or `ANIMEXTS1.0` loop extension | Comments, plain text blocks, other application extensions (XMP among them), anything after the trailer |
| WebP | `VP8X` with its EXIF and XMP flags cleared, `VP8 `, `VP8L`, `ALPH`, `ANIM`, `ANMF` (keeping only `ALPH`, `VP8 ` and `VP8L` inside) and `ICCP` | `EXIF`, `XMP `, unknown chunks, anything past the RIFF size |

A JPEG that isn't upright keeps its orientation as the smallest EXIF block that holds it, and the den accepts it only in exactly this form: `Exif\0\0`, then a big-endian TIFF header (`MM`, 42, the directory at 8), one directory with one entry, orientation (`0x0112`, type SHORT, count 1, the value 2 to 8), and no next directory, 32 bytes in all. An upright JPEG keeps no EXIF.

### Video and audio

Clients strip video and audio before uploading, with the same media module dens run: it copies the file's video and audio streams into a new container and nothing else (see the design's [ffmpeg](../design/media.md#ffmpeg)). A den can't check a container the way it checks an image, so it strips the file again and stores its own copy:

- `type` is what the copy plays as, which its container decides:
  - MP4, QuickTime and AAC become MP4: `video/mp4`, or `audio/mp4` without video.
  - Matroska and WebM become WebM when WebM takes their codecs (`video/webm`, `audio/webm`), else MP4 when it does, else stay Matroska (`video/x-matroska`, `audio/x-matroska`).
  - MP3, FLAC, Ogg and WAV stay as they are: `audio/mpeg`, `audio/flac`, `audio/ogg` and `audio/wav`.
- `size` is the copy's, which must fit the file size limit too.
- `width` and `height` are a video's as it displays: cropped as its container asks, stretched by its pixels' aspect ratio, and turned.
- A video's preview is its first frame, made like an image's. The module decodes H.264 and HEVC, but not VP8, VP9 or AV1, so such a video arrives without `thumb`, and its uploader's client may send one (below).
- A file the module can't read gets `400 invalid_field`, as a damaged image does.

```
POST /api/uploads/{id}/thumb   (body: a JPEG or PNG)   200 file
```

- The image is the video's first frame as the uploader's browser plays it, drawn at the video's size or smaller, at most 16 MiB, and stripped like any image. It must have the video's shape, give or take a pixel of rounding on each side.
- Only the uploader may send one, while the upload waits to be used, for a video without a preview. The den makes its own preview from the image and keeps nothing else of it; the preview counts against the member's space.

### Attachments

```
POST   /api/channels/{id}/messages   {"nonce", "text", "reply_to"?, "attachments"?: ["<upload id>", …], "spoilers"?: ["<upload id>", …]}
PATCH  /api/messages/{id}            {"revision", "text", "attachments"?: ["<file or upload id>", …]}
DELETE /api/uploads/{id}             204
GET    /api/files/{id}               200 the file's bytes
GET    /api/files/{id}/thumb         200 the preview's bytes
GET    /api/me/storage               200 {"used", "den_used"}
GET    /api/me/files?after=<cursor>  200 {"files": [file, …], "messages"?: [message, …], "next"?}
```

- A message holds at most 10 files: the author's own uploads, not yet used. `message.attachments` lists them in the order sent.
- `spoilers` names which of them go as spoilers, each one of the message's uploads, once; anything else gets `400 invalid_field`. The den keeps it on the file, which says `"spoiler": true` to everyone who sees it, and clients show it covered until it's clicked. Once sent it stays: an edit keeps it, and an upload that takes a file's place (`Dens-Replaces`) is a spoiler if the file was. A DM's spoilers go inside its sealed text, never to the den: a DM's send naming any gets `400 invalid_field`. A den older than them, which doesn't say `spoilers` in its info, ignores the field, so a client offers spoilers in a channel only where the den says it keeps them; an older client shows a spoiler uncovered.
- An edit changes the text, and the author's edit can change the files too: `attachments` lists them afterward, in order, as files the message already has and the author's waiting uploads. Files it leaves out are deleted, and `message.updated` lists them in `files`. A change of files marks the message edited. An edit can't leave a message with neither text nor files; a client deletes the message instead.
- An upload made to take a file's place names it, as `Dens-Replaces: <file id>`: one of the member's files, on a message or their profile. The den counts the upload against the member's space less that file's, and it can only take that file's place, in an edit that drops the file or as a picture replacing it. A file has one replacement waiting at most; a newer one drops the older.
- `DELETE /api/uploads/{id}` drops one of the member's uploads waiting to be used, and frees its space at once.
- `GET /api/me/files` lists the member's files on the den, largest first by the space each takes with its preview, at most 50 at a time, `next` continuing the list. Each is a `file` with `created_at`, and `message_id` and `channel_id`, or `"profile": "avatar"` or `"banner"`, or neither while it waits to be used. A channel's file has `excerpt`, the start of its message's text, as a reply's preview has, and `forwarded: true` on a message its author forwarded. A DM's blobs come as sealed files, previews among them, and the page's `messages` carry the DM messages they're on, sealed, for the client to open and name them; a page ends early rather than carry more than 600 KiB of them.
- A file is served as `application/octet-stream` with `Content-Disposition: attachment`. The den never states a type a browser would act on; clients check the bytes before showing anything as an image, video or audio.
- A file answers byte ranges (`Range: bytes=N-`, `206 Partial Content`), and the den decrypts only the chunks a range covers, so a player seeks without fetching the whole file. Its `ETag` is its ID, quoted, since its bytes never change.
- A DM's files are sealed uploads, which its message lists only inside its sealed text (see [DM files](e2ee.md#dm-files)).
- A member may fetch a file when they can see the message it's on, any member may fetch a picture on a profile, and only its uploader may fetch an upload waiting to be used. Anyone else gets `404 not_found`.
- A file's bytes never change under its ID, so clients may keep a copy until an event says it's gone: `message.deleted` lists a message's files, a profile's replaced picture drops out of `member.updated`, `member.left` takes a member's pictures with it, `channel.deleted` takes the channel's, and `messages.expired` those of the messages it ends. After a `ready`, the changes since the client's version say which messages lost files or went (see [What changed while away](sync.md#what-changed-while-away)).
- `GET /api/me/storage` says how much space the member's files take and how much everyone's do, in bytes, previews included.

### Pictures on a profile

```
PATCH /api/me   {"avatar"?: "<upload id>" | "", "banner"?: "<upload id>" | ""}   200 member, with "bio"
```

- An avatar is a square still image, 64 to 1,024 pixels a side. A banner is a still image 300 to 3,000 pixels wide and three times as wide as it is high, give or take 3%. Clients crop pictures to these shapes before uploading them.
- Either may be an animated GIF instead: an avatar then at most 256 pixels a side and 2 MiB, and a banner at most 600 pixels wide and 4 MiB. An animated picture of any other kind, such as an animated WebP or PNG, is refused; a member's own service makes its GIF.
- `""` takes a picture off. The picture a change replaces is deleted.
- A member who leaves or is removed loses their pictures and the uploads they hadn't used.

### Limits

```
limits = {"file_size", "member_storage", "den_storage"}   (bytes)
```

- A new den allows files of 25 MiB, 2 GiB of files per member and 20 GiB for the whole den. The owner changes them with `PATCH /api/den`: a file limit of 1 MiB to 1 GiB, room for at least one file per member, and for at least one member in the den.
- A file over the size limit gets `413 too_large`. One that would take a member past their space gets `507 quota_exceeded`; past the den's space, or with less than 1 GiB left free on the den's disk, `507 den_full`. Space counts files and their previews.
