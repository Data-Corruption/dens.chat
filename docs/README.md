# Documentation site

The public site at dens.chat uses [Hugo](https://gohugo.io/) and
[Hextra](https://imfing.github.io/hextra/). Content, theme configuration, and
deployment configuration are in this directory. Internal developer docs live
in `dev/`; they are not part of the site.

## Local development

Install Go at the version declared in `go.mod` and Hugo Extended. The Hugo
version and checksum used by CI are in in `../scripts/vendor.sh`; Hextra's
minimum Hugo version is declared in `hugo.yaml`. On Linux amd64, run
`./scripts/vendor.sh hugo` from the repository root to obtain the pinned Hugo
binary. Otherwise, see Hugo's [installation guide](https://gohugo.io/installation/).

```sh
cd docs
go mod download
hugo server --buildDrafts --disableFastRender
```

The landing page is its own layout, not a Hextra page: `layouts/home.html`,
with its head, art and components in `layouts/_partials/landing/`, its styles
in `assets/css/landing.css` and two scripts: `assets/js/theme.js`, which runs
first and applies a stored skin, and `assets/js/landing.js` (the skin switcher,
the install button's click, the EULA scrolls, the security cameras, and the
barcode that grows into a forest as you scroll). Light or dark follows the system unless the visitor picks a
skin, which is kept in `localStorage` as `color-theme`, the key Hextra's own
theme toggle uses, so the docs follow it too. Without the script the page still
reads: the EULAs open as plain `<details>` and the forest is simply grown. `content/_index.md` holds only its
front matter. Everything it loads comes from this site: Archivo and Silkscreen
(SIL OFL, licenses beside them) are in `static/fonts/`, and body text uses the
visitor's own Verdana or Tahoma. The click is made with the Web Audio API, so
there's no sound file.

The SVG art (`hero-art.svg`, `pc.svg`, `cloud.svg`, `barcode.svg`,
`burst.svg`) is generated, but committed and edited like any other file.
`hero-art.svg` draws well past both sides of its 1200-wide viewBox: the hero
sizes it to the column and lets it spill out to the page's walls on wide
screens. `static/og.png`, the image a shared link shows, is a render of the
header and hero at 1200 × 630, so render it again when they change.

Documentation is under `content/docs/`; directory structure and front-matter
weights define the Hextra sidebar. Site configuration and top navigation is in
`hugo.yaml`, and small theme overrides belong in `assets/css/custom.css`.

Run the production build before publishing, from the repository root, as the
Docs workflow does:

```sh
./scripts/test.sh -docs
```

It verifies the modules and builds with the pinned Hugo into a cleared `out/`,
as `hugo --gc --minify --panicOnWarning` in production, and fails on code the
highlighter marks as an error, as it does an unquoted URL in PowerShell; then
it checks the site's icon is the app's mark. `refLinksErrorLevel: ERROR` and
`--panicOnWarning` make unresolved Hugo references and build warnings fail.
Hugo does not check arbitrary external links; the browser checks' `site`
check walks the install guide at a phone's width and a desktop's, and checks
the links on its pages and those the app carries into the guides
(`scripts/test/browser/drive-site.mjs`).

Hextra is pinned in `go.mod` and verified by `go.sum`. To update it, choose a
released version, run `hugo mod get github.com/imfing/hextra@<version>`, verify
the modules, inspect the diff, and rebuild.

## Deployment

[`.github/workflows/docs.yml`](../.github/workflows/docs.yml) builds the site
with `./scripts/test.sh -docs` on pushes to `main` and on pull requests that
change the site, workflow, or listed build dependencies, and deploys `out/` to
Cloudflare Workers Static Assets from `main` alone, when the repository
variable `DOCS_ENABLED` is exactly `true`. It can also be run manually.

Before enabling it, add the Cloudflare repository secrets.
[DEPLOYMENT.md](DEPLOYMENT.md) gives the complete setup, local deployment and
preview steps.

## What the site holds

dens.chat is a landing page and a few pages of docs for people using Dens.

**The landing page** says what Dens is in one plain paragraph, then argues for it in three sections, hosting it yourself (01), encrypted and pseudonymous (02) and free (03). Its tone is confident and plain, not combative: the hero's three lines are what Dens isn't (on their terms, under their watch, their product), section 01 says in one paragraph what a den is for and what it isn't, a place where the people running it set the rules and keep them with the moderation tools, and not a place for threats or for material whose making hurt someone, and section 02's table compares Dens with Discord, Signal and Matrix on who sits in between, conceding where they do better. Four notes on how it's built fold under "under the hood". It links into the docs at the install guide (`/docs/install/`, from an Install button beside the hero's caption), the hosting guide (`/docs/hosting/`) and Privacy (`/docs/privacy/`).

**The docs** cover what the app can't show for itself: what happens before Dens runs, outside it (a domain, a router, Caddy), and across devices and time (passwords, recovery, backups), and, once, who can read what.

- They say what things are for rather than which buttons to press, so the page can change without them going stale, and they hold no screenshots, for the same reason.
- They don't repeat the page's settings, which explain themselves a line each, or the commands, which `--help` lists.
- Each fact lives on one page, and the others link to it.
- Readers skim: a step, or a section's first lines, say in a line or two what to do, and why, and what to check, fold under it in Hextra's details, closed, for whoever wants them. What could catch a reader out, such as what removing Dens deletes or who can read what, stays out of the folds.

| Page | Covers |
| --- | --- |
| Docs home (`/docs/`) | Two ways in: you got an invite, or you want to host a den |
| Install (`/docs/install/`) | A choice of platform, four cards and nothing else, Windows, Linux, Flatpak (client only) and Android (client only), each opening a page (`/docs/install/windows/`, `/linux/`, `/flatpak/` and `/android/`, where Android's old `/docs/android/` leads). Each page opens with what it runs on, in a line, then the install itself, with why it needs admin rights or sudo folded beside it; updating and removing, a line or two each; and at its end, closed, which browser works best and checking the installer first. Android's page keeps staying connected and Google's developer verification |
| Usage (`/docs/usage/`, where `/docs/join/` and `/docs/logs/` lead) | What's different about Dens, and nothing a chat app's user knows already: that dens stand alone, with no federation, said first; pasting an invite; a username is for good; the den password and the local password; recovery codes; the DM recovery key; approving a new device; comparing codes; what differs around the den, a line each; writing messages, what the page's Formatting help leaves out; notifications, off until turned on; leaving; and sending logs: turning them up, downloading them, and what's in them |
| Hosting (`/docs/hosting/`, where `/docs/host/` and `/docs/server/` lead) | What a den needs: a computer that stays on, upload speed and a domain; turning hosting on; creating the den; Caddy and the four ports; a call from outside the network as the test; a small VPS where ports can't be forwarded; what differs in running a den, a line each; a den on a Linux server, moved there from a member's computer or started there, reaching it from home, and its commands; a second instance |
| Backups and moving (`/docs/backups/`) | `dens backup` and `dens restore`, Backup in the settings, and the Flatpak's restore; moving to another computer, between Linux and Windows too; handing the den on with `dens den owner-invite`; what's gone when both the password and the computer are |
| Relay (`/docs/relay/`) | What Relay does, its two setups, one call at a time, what each device needs, what's good to know, and what the den learns |
| Privacy (`/docs/privacy/`) | Who can read what, in one table; what Dens doesn't protect against; what leaves your computer; Linux and Windows compared; the short philosophy, with pointers to Signal and Qubes OS |

A page that moves keeps answering at its old path, since released binaries link into the guides: Hugo's alias, from `layouts/alias.html`, which carries the link's anchor along (`/docs/join/#comparing-codes` lands on Usage's), where Hugo's own would drop it.

**The page links to the docs** where what they explain comes up: an i beside the heading, whose tooltip says what the guide covers and whose click opens it in a new tab (`DocsTip`). It stands at the first local password, the recovery codes and DM recovery key shown after joining, Password and recovery in Manage, signing in on a new device, approving one and Add device, comparing codes and Manage's Your code, creating the den, and the settings' Hosting and Backup, and the settings' About links the docs' home. That's v1's onboarding; a fuller one, in the page's own first steps, can come after it.

**The site's checks.** The Docs workflow builds the site on every pull request that changes it, as it does before deploying from `main`, and fails on any warning, on code its highlighter marks as an error, and on an icon that isn't the app's mark (`test.sh -docs`); a pull request that changes only docs runs none of the Release workflow's tests ([release.md's CI jobs](dev/release.md#ci-jobs)). The browser checks' `site` check walks the install guide at 390 pixels by touch and at a desktop's width with a mouse, in Chromium and Firefox, follows its old paths, the Flatpak's section and `/docs/android/`, to where they lead now, and holds it to the phone check's audits, which Hextra's own controls meet through the site's CSS: code's copy buttons shown where nothing hovers, and its heading anchors, which only a hover shows, left out there; and its smallest targets 24 pixels each way.

What the design commits the docs to, by page:

- Install:
  - On Windows, one line for an ordinary terminal, which starts the installer as Administrator once Windows asks; on Linux, downloading the installer and running it with sudo; and checking either installer's signature first as an optional step, folded at the page's end, which on Windows is also how to give it flags (see [Privileged steps and updates](dev/installed.md#privileged-steps-and-updates)).
  - Linux is recommended, since its service sandbox is stronger ([Goals](dev/design.md#goals)), in the Windows page's fold on admin rights.
  - What Windows shows: its prompt to let Windows PowerShell, which Microsoft signs, make changes, and no SmartScreen check, since nothing a browser downloaded runs; and that Smart App Control, where it's on, blocks Dens, and where to turn it off ([Weaker than Linux](dev/installed.md#weaker-than-linux-stated-in-the-docs)).
  - The Flatpak, on a page of its own (`/docs/install/flatpak/`), which `dens install`'s refusals on NixOS and SteamOS link to; releases before v1.10.0 link `/docs/install/#the-flatpak`, which lands on the choice, the Flatpak's card in view: that it runs as the member, only while they're signed in, and hosts no den; its link, with `flatpak install` as the fallback; that installing for the whole system asks for the admin password, and that GNOME Software calls Dens potentially unsafe for X11, which push to talk under X11 needs; that a KDE with no wallet asks to make one at the first Remember, where Classic, with the sign-in password, is the choice that works; that updates come through the software center; and an alias for its commands (`alias dens='flatpak run --command=dens chat.dens.Dens'`) ([flatpak.md](dev/flatpak.md)).
  - Android: installing the APK past the browser's warning, Galaxy's Auto Blocker and Play Protect; what keeps it connected; that it updates itself, asking first; and why Dens isn't in Google Play and doesn't register for Google's developer verification, then the advanced flow, chosen for good ([android.md](dev/android.md)).
- Usage: what's unusual about Dens, in plain words: the local password and den passwords, recovery codes and the DM recovery key, den IDs, approving new devices and comparing codes. Onboarding says the same things, shorter, in the same words.
- Hosting:
  - Hosting starts from the settings, with nothing to choose at install ([Install and service account](dev/installed.md#install-and-service-account)).
  - Which ports to forward but not how, since routers vary too much, and a small VPS as the easy alternative ([Networking and deployment](dev/design.md#networking-and-deployment)).
  - A minimal Caddyfile, and Caddy as a service on both platforms, which covers ports 80 and 443 ([Caddy](dev/design.md#caddy)).
  - Calls need the den's name to point at its machine, or the router in front of it: a tunnel or CDN carries a den's HTTPS but not its calls. The owner's own calls work before any port is forwarded, so the test is a call from outside the network ([Media addresses](dev/design/sfu.md#media-addresses)).
  - How to raise the call limits on a faster connection ([Limits for calls](dev/design/sfu.md#limits-for-calls)).
  - A backup keeps what the den held when it was made, past the auto-delete period, until the backup is deleted ([Retention](dev/design/sync.md#retention)).
  - Dens is one program, its database and media tools built in, so a den needs only Caddy beside it.
  - What a den costs in upload: members pick up where they left off rather than reloading, presence goes out in batches, and pictures go as previews until opened ([Sync and bandwidth](dev/design/sync.md#sync-and-bandwidth)).
  - A second den is a second instance, with ports of its own ([Install and service account](dev/installed.md#install-and-service-account)). An install that hosts updates as one binary with its client, and a den that can't wait on its owner's updates moves to an install of its own ([Versioning and compatibility](dev/design.md#versioning-and-compatibility)).
- Privacy:
  - The den can read channels and the files in them, and sees who DMs whom and when, and who's in a call; DMs, group DMs, notes to self and calls are end-to-end encrypted, but a den running an older Dens could listen to calls and watch screen shares ([Privacy model](dev/design.md#privacy-model), [End-to-end encryption](dev/design/e2ee.md), [Sealed calls](dev/design/e2ee.md#sealed-calls)). The docs say the den rather than its owner: whoever or whatever gets hold of the den's computer, someone else who uses it or malware on it, can do the same.
  - Files that aren't photos, videos or audio, such as documents and archives, go as they are, with whatever they carry inside ([Metadata stripping](dev/design/files.md#metadata-stripping)).
  - An update check asks the release host for the latest version number, which shows it the install's IP address ([Privileged steps and updates](dev/installed.md#privileged-steps-and-updates)), and the Android app downloads the update it finds from there too ([android.md's Updates](dev/android.md#updates)); a member with YouTube's players on shows YouTube their IP address ([YouTube players](dev/design/embeds.md#youtube-players)), and one with Steam's cards on shows Steam theirs ([Steam's cards](dev/design/embeds.md#steams-cards)); a browser new to Dens asks about both at its first run ([Link previews](dev/design/embeds.md#link-previews)).
  - Windows is weaker than Linux, as [Weaker than Linux](dev/installed.md#weaker-than-linux-stated-in-the-docs) lists.
  - The installed service keeps its data under its own account, out of reach of the member's programs; the Flatpak runs as the member, so their programs can read it ([flatpak.md](dev/flatpak.md)).
  - The short philosophy section and its recommendations: Linux over Windows, Ungoogled Chromium, no identifying details for low-stakes use, and Signal, Qubes OS and similar tools for higher stakes, none of them bulletproof ([Privacy model](dev/design.md#privacy-model)).
