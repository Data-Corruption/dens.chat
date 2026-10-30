# Releases

How Dens releases are built, signed, published and recovered, and the Flatpak
published from each. A new version heading in `CHANGELOG.md`, pushed to
`main`, triggers a release through GitHub Actions.

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
dens.apk
releases/
  v1.5.0/
    version
    linux-amd64.gz
    linux-arm64.gz
    windows-amd64.exe.gz
    windows-arm64.exe.gz
    android-arm64.apk
    ffmpeg-source.tar.xz
    checksums.txt
    checksums.txt.cosign.bundle
flatpak/
  chat.dens.Dens.flatpakref
  dens.flatpakrepo
  repo/
.state/
  promotions/
  flatpak
```

`ffmpeg-source.tar.xz` holds FFmpeg's source, as the binaries include it, with zlib's and libaom's, the driver and the script that builds the module (M1.8, M5.4), as the LGPL asks of a binary that includes FFmpeg.

`android-arm64.apk` is the Android app (M12.4), `chat.dens.Dens`, in each
release from v1.5.0 (`ANDROID_FIRST_RELEASE` in `scripts/ci/release.sh`), and
listed in the release's checksums as the binaries are: a release before it has
none, and still verifies. Its version code is the release's version, vX.Y.Z as
X×1,000,000 + Y×1,000 + Z, so each release's is higher, as Android needs of an
update. `dens.apk` at the root is a copy of the promoted release's, which the
install guide links to, since a release's own paths name its version; the
publisher replaces it as it promotes each release, and reads it back.

`flatpak/` holds Dens's Flatpak (M9.4): `repo/`, its OSTree repository, with
the builds of the promoted release's Linux binaries for x86_64 and aarch64,
and the two links members install from, which carry its key. `.state/` holds
the publisher's records: when each release was promoted, and what the last
Flatpak publication left.

This layout and the signing identity below are fixed once the first release
ships: installed copies depend on both.

## Signing identity

Releases are signed with keyless cosign from GitHub Actions. The identity
includes the repository and `.github/workflows/release.yml@refs/heads/main`.
Never rename or move that workflow file; its contents can change. Changing the
identity breaks verification for every existing installation.

GitHub Actions are pinned to commit SHAs, and the Flatpak job's container
image to its digest. Only the release job requests `contents: write` (for
tags) and `id-token: write` (for signing).

The Flatpak is signed with a GPG key instead, since Flatpak checks no other
kind of signature: one Ed25519 key, which every client checks, that never
expires, since a key that expired would fail every Flatpak's check, the
summary's that would bring them a new key included. Its secret half only the
`flatpak` job reads (see The Flatpak's key).

The Android app's APK is signed with a key of its own too, the APK's key,
since Android takes an update only signed with the key the app was installed
with: one EC P-256 key, in a PKCS #12 keystore whose certificate holds for 100
years, which only the `release` job reads, in the `android` environment (see
The APK's key). Naming the environment changes what the job's cosign
certificates say of the job, but not the identity installs check them
against, which is the workflow's path and ref. The app checks the checksums'
cosign bundle itself, where no cosign runs, against the release identity its
build carries and the Sigstore trusted root it embeds
(`internal/platform/release/trusted_root.json`, from Sigstore's TUF
repository), and the release pipeline checks each new release's the same way
before publishing it.

## One-time setup

### Cloudflare R2

Use a domain in the same Cloudflare account as the bucket.

1. In **Storage & databases → R2 object storage**, create a bucket.
2. Open the bucket's **Settings → Custom Domains → Add** and attach the release
   hostname.
3. In the domain's **Rules**, create a cache rule matching that hostname and set
   **Bypass cache**. The root `version` file and the Flatpak's summaries must
   stay current.
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

### The Flatpak's key

Make it on your own machine, in a GnuPG home of its own, so it never mixes
with your other keys, and without a passphrase: the job couldn't type one, and
the environment is what guards it:

```sh
export GNUPGHOME=$(mktemp -d)
gpg --batch --passphrase '' --quick-gen-key 'Dens Flatpak' ed25519 sign never
gpg --armor --export > dens.asc
gpg --armor --export-secret-keys > dens-flatpak.secret.asc
cp "$GNUPGHOME"/openpgp-revocs.d/*.rev dens-flatpak.rev
```

1. **Keep the secret key and its revocation certificate offline, in two
   places**, such as a password manager and a USB drive kept elsewhere. A key
   that's lost can't be replaced without every member installing Dens again
   (see Replacing the Flatpak's key).
2. **Commit `dens.asc`**, the public half, as `scripts/flatpak/dens.asc`. The
   links carry it, and the publisher refuses to sign with any other key.
3. **Settings → Environments → New environment**, named `flatpak`, before any
   workflow names it, since GitHub makes one with no rules for a workflow that
   names one that doesn't exist. Under **Deployment branches and tags**, choose
   **Selected branches and tags** and add the branch `main`. A job reads an
   environment's secrets only by naming it, and only on a branch it allows, so
   no workflow changed on another branch, or in a pull request, can read the
   key, as it could a repository secret. It asks for no reviewer's approval,
   since a release already takes a merge to `main`.
4. In the environment, **Add environment secret** `FLATPAK_GPG_KEY`, with the
   whole of `dens-flatpak.secret.asc`.
5. Delete the copies left on the machine: `gpgconf --kill gpg-agent`, then
   `rm -r "$GNUPGHOME"`, and the two files once they're in their two places.

### The APK's key

Make it once, on your own machine, before the first release with the APK, with
the `keytool` of the JDK `vendor.sh` pins (which fetches it the first time), in
a directory of its own, starting from the repository's root. Its password, one
line with no newline at its end, only opens the keystore: the environment is
what guards the key in CI, and the password manager and the drive offline.

```sh
keytool=$(./scripts/vendor.sh android | sed -n 's/^android=//p')/jdk/bin/keytool
cd "$(mktemp -d)" && umask 077
head -c 32 /dev/urandom | base64 | tr -d '\n' > dens-android.password
"$keytool" -genkeypair -keystore dens-android.p12 -storetype PKCS12 \
  -storepass:file dens-android.password -alias dens \
  -keyalg EC -groupname secp256r1 -validity 36500 -dname "CN=Dens"
"$keytool" -exportcert -rfc -keystore dens-android.p12 \
  -storepass:file dens-android.password -alias dens > release.pem
"$keytool" -printcert -file release.pem | grep SHA256
```

1. **Keep `dens-android.p12` and `dens-android.password` offline, together, in
   two places**, such as a password manager and a USB drive kept elsewhere.
   Every update must carry this key's signature: if every copy is lost, no
   update reaches an installed app, and every member uninstalls Dens, which
   deletes its data on the phone, and installs it again (see Losing or
   leaking the APK's key).
2. **Commit `release.pem`**, the key's certificate, as `android/release.pem`.
   A release's build refuses an APK signed by any other key.
3. **Settings → Environments → New environment**, named `android`, before
   anything that names it reaches `main`: the `release` job names it, and
   runs on every push to `main`, and GitHub makes one with no rules for a
   workflow that names one that doesn't exist. Under **Deployment branches
   and tags**, choose **Selected branches and tags** and add the branch
   `main`, as for the `flatpak` environment, with no reviewer.
4. In the environment, **Add environment secret** `ANDROID_KEYSTORE`, with the
   keystore in base64 (`base64 -w0 dens-android.p12`), and
   `ANDROID_KEYSTORE_PASSWORD`, with the password (`cat dens-android.password`),
   and nothing after it.
5. Delete the copies left on the machine once they're in their two places:
   `rm -r` the directory.

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

`scripts/ci.sh` has three commands. The workflow runs `--plan`, waits for the
tests it requests, then runs `--execute`, then `--flatpak`. Upload and
recovery code lives under `scripts/ci/`.

```sh
./scripts/ci.sh --plan     # decide which tests to run, and whether to release
./scripts/ci.sh --execute  # publish a release or finish an interrupted attempt
./scripts/ci.sh --flatpak  # publish the promoted release's Flatpak, or finish an interrupted attempt
```

**Planning.** The plan reads the newest version heading in `CHANGELOG.md`,
checks the release host and Git tag, and verifies any existing release files
with their signatures and checksums.

**Before the first release.** While `CHANGELOG.md` has no version heading and
the repository has no version tag, the plan asks for the same validation a pull
request gets and no release job, and needs no R2 credentials. Merges to `main`
are tested and nothing is published. Once a version tag exists, a missing
heading fails the plan.

- If the version has no Git tag yet, the E2E jobs run: Linux's, Windows' and
  the Flatpak's. Files
  left by an interrupted upload do not count as a completed release.
- If the version is already tagged, those jobs are skipped. The publisher
  checks for installer changes and tests changed installers before replacing
  them.

Pull requests always run validation and never contact the release host.

**Publication order.**

1. Build, package and sign the application, unless verified files for this
   version are already on the release host. The build runs no tests of its
   own: it only ever builds a version this commit's test jobs passed,
   `linux-tests`' Go suite among them. Release builds link with `-s -w`,
   which drops the symbol table and DWARF, about a third of the binary (89.4 MB
   to 56.1 MB for linux-amd64), while a panic still names its functions and
   lines, from Go's own line table; development builds keep both, for
   debuggers (M9.1). From v1.5.0 the build makes the APK too, around Dens
   built for Android, signs it with the APK's key, and refuses it unless that
   key alone signed it, as `android/release.pem` names it. Once cosign has
   signed the checksums, the build checks the signature as the Android app
   will (`TestReleaseChecksOutForTheApp`), which reads cosign's bundle itself:
   a change in what cosign writes, or in Sigstore's trusted root, stops the
   release before anything is published, rather than every app's next update.
2. Upload the files to `releases/<version>/`, download them again and check
   their signatures and checksums.
3. Generate the installer scripts from the project settings. Test changed
   installers against both the new version and the version currently offered,
   then sign and upload them. The lifecycle harness's flows check this tree's
   behavior, so they run only against a release this run built; any other,
   such as the version currently offered, or the one a push without a new
   version reuses, is installed through the installer alone, verifying its
   signature, and uninstalled (`--installer-only`).
4. Update the root `version` file to make the new release available, then
   `dens.apk`, the promoted release's APK.
5. Record the release commit and publication time, push the Git tag, and remove
   old releases under the retention rule.

An installer's signature bundle is uploaded before its script. While the pair is
being replaced, a download can briefly see a mismatched pair; verification
rejects it. The publisher keeps verified temporary copies so a retry can finish
the replacement. Unchanged installers keep their existing signatures.

**The Flatpak.** Once the release job has succeeded, the `flatpak` job
publishes the promoted release's Flatpak (`scripts/ci/flatpak.sh`), in a Debian
13 container, the system the Flatpak e2e and the VMs' builds make it on:

1. Import `FLATPAK_GPG_KEY` into a GnuPG home of the run's own, and refuse
   anything but the secret halves of the keys `scripts/flatpak/dens.asc` holds
   that aren't revoked.
2. Read the root `version`. A release before v1.2.0, the first whose binary
   runs as the Flatpak, has nothing to publish.
3. Copy `flatpak/repo/` down and pull its builds into a fresh local
   repository, which checks every commit against the committed keys and every
   object against its checksum. A repository that fails is refused, never
   repaired. A host with no summary, which no client could have installed
   from, gets a new repository.
4. Done, where the repository holds the version for both architectures and
   `.state/flatpak` says the last publication left the summaries the host
   serves; refused, where it holds a newer version than the promoted one, which
   a Flatpak would update to and then refuse the data a newer one wrote.
5. Download the release's checksums, their signature and its Linux binaries,
   and check them as an install does.
6. Commit each architecture missing that version, with the delta from the
   build before by bsdiff past OSTree's limit, and write the summary and the
   other deltas, signed, keeping each architecture's last four builds, which
   a member can roll back to.
7. Upload objects and deltas, then the summaries' parts, then the refs, then
   each summary index before its summary, and delete what the new summaries
   no longer name, last. Clients read the old repository or the new one
   throughout, but for the moment between a summary and its signature, which
   they retry past. Read the summaries back, check them, upload the links
   that changed, and record the publication in `.state/flatpak`, last.

Installers read the root `version` file once and use that version throughout an
install. A retry can run on a fresh runner: it downloads and verifies the
release files to learn which steps are complete. It stops on damaged files or
conflicting records instead of overwriting them, and it refuses to move an
existing tag or make an older version current again.

**Retention.** The publisher keeps the two newest promoted releases, and any
older release until at least 24 hours after its promotion. It also keeps the
first release, v1.0.0 (`DENS_FIRST_RELEASE` in `scripts/vendor.sh`), for good:
installed copies hold what it wrote, so the lifecycle harnesses install it and
update from it to every new release.

**Release source.** An install records its effective release URL, including a
`--release-url` the installer was given, in the instance config, and `dens
update` uses that saved source. A missing or invalid source prevents updating
instead of falling back to the public host. Only flags change the source, or
skip verifying, never the environment, and a binary that carries the signing
identity refuses to skip it (M8). Nor does an install move to an older release
than the one it has, so a host or mirror that served an older signed release
couldn't roll installs back.
Signatures cover artifact bytes and the workflow identity, so unchanged
artifacts also verify when served from a mirror.

## CI jobs

| Job | What it does |
|---|---|
| `release-plan` | Runs `ci.sh --plan` on pushes to `main`; on pull requests, asks for validation and no release |
| `linux-tests` | Linux Go tests, the third-party notices, shell lint, the page's tests and the release tests |
| `linux-e2e` | The lifecycle e2e, installer tests included, across the supported distros, in Incus containers |
| `linux-den-e2e` | A member joins a den through Caddy, chats, calls and is banned, in two Incus containers |
| `windows-e2e` | Native Go tests, PowerShell parsing, and Windows installer tests |
| `flatpak-e2e` | The Flatpak's publisher, then the Flatpak itself, published by it and run for a member, in Incus containers; releases wait for it |
| `android-build` | The Android app as a development build (`test.sh -android`): Go vetted for Android, both APKs, and the host package's tests for the emulator; releases wait for it |
| `release` | Runs `ci.sh --execute` on pushes to `main` when the plan asks for a release and all requested tests pass, in the `android` environment, whose key signs the APK |
| `flatpak` | Runs `ci.sh --flatpak` in the `flatpak` environment once `release` succeeds |

A failed plan, failed test or cancellation blocks publication. Tests skipped at
the plan's request let publication continue. A failed `flatpak` job leaves the
release alone. Runs on `main`, and their publications, serialize through the
workflow's concurrency group; a pull request's newer commit replaces its older
run.

## Retry an interrupted release

For a runner, network or upload interruption, use **Re-run failed jobs** on the
original workflow run, or `gh run rerun RUN_ID --failed`. The publisher checks
what was already uploaded and verified, then continues. Keep the original source
and version for the retry. Do not create the tag by hand or push a dummy commit
to restart publication.

If the code or tests need a fix, commit it with a new version heading. Never
replace published bytes or move an existing tag.

A failed `flatpak` job is retried the same way, and every later push to `main`
finishes it too, since each run publishes what the repository is missing. A fix
to the publisher needs no new version.

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
- **A Flatpak repository that doesn't check out against its keys.** Find out
  why first: an object changed on the host means someone could write to the
  bucket, so replace the R2 credentials. Then delete `flatpak/repo/` and
  `.state/flatpak` and rerun the job, which publishes the promoted release
  afresh. Members' Flatpaks update to it as to any build; the older builds
  they could roll back to are gone.
- **A Flatpak repository newer than the promoted release.** The root `version`
  never moves back, so something else wrote it. Investigate before deleting
  anything.

## Replacing the Flatpak's key

Every Flatpak takes the keys a signed summary names (`xa.gpg-keys`) at its
next update, so a key can be replaced with no member installing Dens again, as
long as the old one still signs while the new one spreads. The publisher's
tests run these steps.

1. **Add the new key.** Make it as above. Commit `scripts/flatpak/dens.asc`
   holding both public keys, and set `FLATPAK_GPG_KEY` to both secret keys,
   one file after the other. Each publication now signs with both, adds the
   new key's signature to every build the repository keeps, and names both in
   its summary, and the links carry both.
2. **Wait** while members' Flatpaks update, over a few releases.
3. **Retire the old key.** Import its revocation certificate (delete the `:`
   that starts its `BEGIN` line first) into a GnuPG home holding the old public
   key, export that key, revoked, and commit it in `dens.asc` beside the new
   one. `FLATPAK_GPG_KEY` holds the new key alone. The summary carries the
   revocation, and a Flatpak that takes it refuses what the old key alone
   signs.

A Flatpak that didn't update between the first step and the third can't check
the new summaries, and its member installs Dens again from the site's link.

If the key leaks, take the same steps without waiting long: the key alone
can't publish, which also takes writing to the bucket or posing as the release
host, but whoever holds it could publish once they can. If every copy of the
key is lost, nothing can sign a summary members' Flatpaks trust: publish under
a new key, and every member installs Dens again. That's why the key is kept in
two places.

## Losing or leaking the APK's key

Android checks each update's signature against the key the app was installed
with. APK Signature Scheme v3 lets that key hand over to a new one: `apksigner
rotate` makes a signing lineage, the old key's signature over the new, and an
update signed with the new key, carrying the lineage, installs over the old;
the lineage's capabilities say what the old key may still sign after. So a key
that leaks can be replaced while it's still in hand. The key alone can't reach
installed apps, whose updater takes only an APK the release's signed checksums
name, but whoever holds it can sign an APK that a member who downloads it from
elsewhere installs over Dens. If every copy of the key is lost, nothing can
sign an update installed apps take: publish under a new key, and every member
uninstalls Dens and installs it again, losing what it kept on the phone.
That's why it's kept in two places.

## Testing release changes

After touching `scripts/ci/`, `scripts/ci.sh` or the installers, run
`./scripts/test.sh -release`. It uses an rclone local backend and a cosign
stand-in to exercise interruption, resume, promotion, installer pair recovery,
immutable tags, retention, and the APK's place in releases from v1.5.0 and
at the root, without a real bucket. The Android app's updater is tested
against releases of its own (`scripts/test/android/releases.sh`), in the
Android e2e, whose checksums a Sigstore of the tests' own signs
(`internal/platform/release/sigstoretest`). After touching the Flatpak's
publisher (`scripts/ci/flatpak.sh`, `scripts/flatpak.sh`), run
`./scripts/test.sh -flatpak-e2e`, whose first stage tests it the same way (see
[flatpak.md](flatpak.md)).
