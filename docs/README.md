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
the EULA scrolls, the security cameras, and the barcode that grows into a forest
as you scroll). Light or dark follows the system unless the visitor picks a
skin, which is kept in `localStorage` as `color-theme`, the key Hextra's own
theme toggle uses, so the docs follow it too. Without the script the page still
reads: the EULAs open as plain `<details>` and the forest is simply grown. `content/_index.md` holds only its
front matter. Everything it loads comes from this site: Archivo and Silkscreen
(SIL OFL, licenses beside them) are in `static/fonts/`, and body text uses the
visitor's own Verdana or Tahoma.

The SVG art (`hero-art.svg`, `pc.svg`, `cloud.svg`, `barcode.svg`,
`burst.svg`) is generated, but committed and edited like any other file.
`hero-art.svg` draws well past both sides of its 1200-wide viewBox: the hero
sizes it to the column and lets it spill out to the page's walls on wide
screens. `static/og.png`, the image a shared link shows, is a render of the
header and hero at 1200 × 630, so render it again when they change.

Documentation is under `content/docs/`; directory structure and front-matter
weights define the Hextra sidebar. Site configuration and top navigation is in
`hugo.yaml`, and small theme overrides belong in `assets/css/custom.css`.

Run the production build before publishing:

```sh
go mod verify
HUGO_ENV=production HUGO_ENVIRONMENT=production \
  hugo --gc --minify --panicOnWarning
```

The build writes to `out/`. `refLinksErrorLevel: ERROR` and
`--panicOnWarning` make unresolved Hugo references and build warnings fail.
Hugo does not check arbitrary external links.

Hextra is pinned in `go.mod` and verified by `go.sum`. To update it, choose a
released version, run `hugo mod get github.com/imfing/hextra@<version>`, verify
the modules, inspect the diff, and rebuild.

## Deployment

[`.github/workflows/docs.yml`](../.github/workflows/docs.yml) builds and deploys
`out/` to Cloudflare Workers Static Assets when the repository variable
`DOCS_ENABLED` is exactly `true`. It runs on pushes to `main` that change the
site, workflow, or listed build dependencies, and can also be run manually.

Before enabling it, add the Cloudflare repository secrets.
[DEPLOYMENT.md](DEPLOYMENT.md) gives the complete setup, local deployment and
preview steps.
