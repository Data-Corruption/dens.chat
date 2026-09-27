# Deploying the documentation site

The site builds with Hugo and deploys the generated `out/` directory to
[Cloudflare Workers Static Assets](https://developers.cloudflare.com/workers/static-assets/).
No Worker script or separate Pages project is needed.

## Files and build inputs

- `hugo.yaml` sets the canonical site URL, title, navigation, and `out/` publish directory.
- `go.mod` and `go.sum` pin and verify the Hextra theme.
- `wrangler.jsonc` names the Worker and points its assets at `./out`. Its
  `404-page` setting serves Hugo's `404.html` for missing pages.
- `../scripts/vendor.sh` pins Hugo Extended (including its archive checksum)
  and Wrangler. The workflow obtains Hugo through this script and uses its
  exact Wrangler version.
- `../.github/workflows/docs.yml` verifies modules, builds with warnings treated
  as errors, and deploys when `DOCS_ENABLED` is `true`.

## Worker name and site URL

`wrangler.jsonc` names the Worker `dens-site`, and `hugo.yaml` sets `baseURL`
to `https://dens.chat/`. Deployments update the Worker with that name, so
changing the name later targets a different Worker. `baseURL` controls
generated canonical URLs, sitemap entries and social links; it does not create
a domain or change Cloudflare routing. Attach the domain to the Worker in the
Cloudflare dashboard.

Keep `publishDir: out` and `assets.directory: ./out` in sync if you change the
output directory.

## Connect Cloudflare and GitHub

1. **Copy your Cloudflare account ID.** Find it in the Cloudflare dashboard for
   the account that will own the Worker.
2. **Create an API token.** Under My Profile → API Tokens, use the
   **Edit Cloudflare Workers** template and restrict Account Resources to that
   account. If you use a custom domain, scope Zone Resources to its zone.
3. **Add repository secrets.** In GitHub → Settings → Secrets and variables →
   Actions, create `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN` with those
   values. These credentials belong to the destination repository's account.
4. **Enable deployment.** In the same settings area, add the repository variable
   `DOCS_ENABLED` with the exact value `true`. Until then, the workflow's gate
   job reports that deployment is disabled. The release workflow's `CI_ENABLED`
   variable is independent.
5. **Deploy.** Push a site/config change to `main`, or open Actions → Docs →
   Run workflow. The first deployment creates the named Worker. Later
   deployments update it. If your default branch has another name, update
   `on.push.branches` in `.github/workflows/docs.yml`.
6. **For a custom domain**, open Workers & Pages → your Worker → Settings →
   Domains & Routes → Add → Custom domain, and enter the hostname used in
   `baseURL`. The zone must be active in your Cloudflare account. Cloudflare
   manages its DNS record and certificate; see the
   [custom domain guide](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/).
   If you change `baseURL` after the first build, rebuild and deploy again.

## Local deploys and previews

Install Go, Hugo Extended, and Node.js (needed by Wrangler). From the repository
root, use Bash so local deploys read the same Wrangler pin as CI:

```sh
source scripts/vendor.sh
cd docs
go mod download
go mod verify
HUGO_ENV=production HUGO_ENVIRONMENT=production \
  hugo --gc --minify --panicOnWarning
npx --yes "wrangler@${WRANGLER_VERSION}" deploy --dry-run
npx --yes "wrangler@${WRANGLER_VERSION}" deploy
```

Wrangler can prompt for browser login locally, or use the
`CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` environment variables.
`--dry-run` packages and validates without deploying.

There are no automatic pull-request previews. After the initial deployment,
run `npx --yes "wrangler@${WRANGLER_VERSION}" versions upload` from the same
shell and directory to upload a version without changing production. Enable
`preview_urls` in `wrangler.jsonc` if needed; see Cloudflare's
[preview URL documentation](https://developers.cloudflare.com/workers/versions-and-deployments/preview-urls/).
The generated site still uses the configured `baseURL` for canonical URLs.

## Site identity

- `hugo.yaml`: title, both descriptions and the GitHub navigation URL.
- `layouts/_partials/custom/footer.html`: author name and profile link.
- `static/favicon.svg`: the site icon.
