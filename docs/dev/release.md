# Releases

How Dens releases are built, signed, published and recovered. A new version
heading in `CHANGELOG.md`, pushed to `main`, triggers a release through GitHub
Actions.

## What gets published

Each version has its own directory on the release host. Once a complete release
has been uploaded and verified, its files are never replaced. The root `version`
file tells installers which version to download. Root installer scripts can be
updated separately from the application binaries.

Paths are relative to `RELEASE_URL` (`https://releases.dens.chat/`, set in the
project block of `scripts/build.sh`). If the URL has a path, all publication and
cleanup stay inside that path in the bucket.

```text
install.sh
install.sh.cosign.bundle
install.ps1
install.ps1.cosign.bundle
version
releases/
  v1.0.0/
    version
    linux-amd64.gz
    linux-arm64.gz
    windows-amd64.exe.gz
    windows-arm64.exe.gz
    checksums.txt
    checksums.txt.cosign.bundle
```

This layout and the signing identity below are fixed once the first release
ships: installed copies depend on both.

## Signing identity

Releases are signed with keyless cosign from GitHub Actions. The identity
includes the repository and `.github/workflows/release.yml@refs/heads/main`.
Never rename or move that workflow file; its contents can change. Changing the
identity breaks verification for every existing installation.

GitHub Actions are pinned to commit SHAs. Only the release job requests
`contents: write` (for tags) and `id-token: write` (for signing).

## One-time setup

### Cloudflare R2

Use a domain in the same Cloudflare account as the bucket.

1. In **Storage & databases → R2 object storage**, create a bucket.
2. Open the bucket's **Settings → Custom Domains → Add** and attach the release
   hostname.
3. In the domain's **Rules**, create a cache rule matching that hostname and set
   **Bypass cache**. The root `version` file must stay current.
4. From the R2 overview, open **Manage API tokens** and create a token with
   **Object Read & Write** access to this bucket. Save the access key ID and
   secret access key, and note the account ID and bucket name.

`RELEASE_URL` must end with `/`. Path segments may contain letters, digits, `-`,
`_`, `.` and `~`; no empty segments, `.` or `..`, URL escapes, credentials,
query or fragment. `R2_BUCKET` is always just the bucket name. Other
S3-compatible hosts need changes to the publication code under `scripts/ci/`.

### GitHub Actions

1. **Settings → Actions → General → Workflow permissions**: select **Read and
   write permissions**.
2. **Settings → Secrets and variables → Actions → Secrets**: add
   `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_ACCOUNT_ID` and `R2_BUCKET`.
3. **Variables**: add `CI_ENABLED` with the value `true`. Until it is set, only
   the small `ci-gate` job runs.

## Publish

1. Check all production targets locally: `./scripts/build.sh --prod-all`.
2. Add a version heading at the top of `CHANGELOG.md`, greater than the current
   version:

   ```md
   ## [v1.0.0] - 2026-09-06

   Initial release.
   ```

3. Commit and push or merge to `main`, then watch the release workflow in the
   **Actions** tab.

Check the result with `curl -fsSL https://releases.dens.chat/version`, install
on a test machine, and after a second release, update an older installation
with `dens update` and check that the new version starts.

## How publication works

`scripts/ci.sh` has two commands. The workflow runs `--plan`, waits for the
tests it requests, then runs `--execute`. Upload and recovery code lives under
`scripts/ci/`.

```sh
./scripts/ci.sh --plan     # decide which tests to run
./scripts/ci.sh --execute  # publish a release or finish an interrupted attempt
```

**Planning.** The plan reads the newest version heading in `CHANGELOG.md`,
checks the release host and Git tag, and verifies any existing release files
with their signatures and checksums.

- If the version has no Git tag yet, the Linux and Windows E2E jobs run. Files
  left by an interrupted upload do not count as a completed release.
- If the version is already tagged, those jobs are skipped. The publisher
  checks for installer changes and tests changed installers before replacing
  them.

Pull requests always run validation and never contact the release host.

**Publication order.**

1. Build, package and sign the application, unless verified files for this
   version are already on the release host.
2. Upload the files to `releases/<version>/`, download them again and check
   their signatures and checksums.
3. Generate the installer scripts from the project settings. Test changed
   installers against both the new version and the version currently offered,
   then sign and upload them.
4. Update the root `version` file to make the new release available.
5. Record the release commit and publication time, push the Git tag, and remove
   old releases under the retention rule.

An installer's signature bundle is uploaded before its script. While the pair is
being replaced, a download can briefly see a mismatched pair; verification
rejects it. The publisher keeps verified temporary copies so a retry can finish
the replacement. Unchanged installers keep their existing signatures.

Installers read the root `version` file once and use that version throughout an
install. A retry can run on a fresh runner: it downloads and verifies the
release files to learn which steps are complete. It stops on damaged files or
conflicting records instead of overwriting them, and it refuses to move an
existing tag or make an older version current again.

**Retention.** The publisher keeps the two newest promoted releases, and any
older release until at least 24 hours after its promotion.

**Release source.** An install records its effective release URL, including an
`APP_RELEASE_URL` override, in the instance config, and `dens update` uses that
saved source. A missing or invalid source prevents updating instead of falling
back to the public host.
Signatures cover artifact bytes and the workflow identity, so unchanged
artifacts also verify when served from a mirror.

## CI jobs

| Job | What it does |
|---|---|
| `release-plan` | Runs `ci.sh --plan` on pushes to `main`; enables validation on pull requests |
| `linux-e2e` | Linux Go tests, shell lint, release tests, and installer tests across distros |
| `windows-e2e` | Native Go tests, PowerShell parsing, and Windows installer tests |
| `release` | Runs `ci.sh --execute` on pushes to `main` after all requested tests pass |

A failed plan, failed test or cancellation blocks publication. Tests skipped at
the plan's request let publication continue.

## Retry an interrupted release

For a runner, network or upload interruption, use **Re-run failed jobs** on the
original workflow run, or `gh run rerun RUN_ID --failed`. The publisher checks
what was already uploaded and verified, then continues. Keep the original source
and version for the retry. Do not create the tag by hand or push a dummy commit
to restart publication.

If the code or tests need a fix, commit it with a new version heading. Never
replace published bytes or move an existing tag.

Example: v1.0.0 became available but its workflow failed to push the tag, and
v1.1.0 has since been published. Retrying the v1.0.0 workflow skips E2E, pushes
the missing v1.0.0 tag and runs the usual cleanup; installers keep getting
v1.1.0.

### Repeated integrity errors

Read the CI error and inspect the bucket before changing anything. Preserve
remote objects and logs while you investigate.

- **A complete but invalid release prefix that was never promoted.** First prove
  it: no root pointer naming it, no promotion marker, no Git tag. Then delete
  the whole `releases/<version>/` prefix and rerun the same workflow.
- **An incomplete or invalid prefix that was promoted or tagged.** Restore the
  exact original signed objects from trusted storage. Never build different
  bytes under a version someone may be running. If they can't be restored,
  treat it as a release incident and publish a new forward version.
- **An invalid root installer pair with no matching verified staging.** Look at
  the script and bundle first. Either restore a known-good pair deliberately, or
  remove both objects and rerun after reviewing the replacement candidate.
- **A Git tag already on a different commit.** Never let automation force-move
  it. Correct the release with a new version.
- **An invalid promotion marker.** Restore or reconstruct it only from trusted
  release records: its commit decides the tag target and its timestamp decides
  retention.

## Testing release changes

After touching `scripts/ci/`, `scripts/ci.sh` or the installers, run
`./scripts/test.sh -release`. It uses an rclone local backend and a cosign
stand-in to exercise interruption, resume, promotion, installer pair recovery,
immutable tags and retention without a real bucket.
