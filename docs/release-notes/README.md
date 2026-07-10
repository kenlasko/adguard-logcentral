# Releases and release notes

Releases are cut by manually running the **Release** workflow
(`.github/workflows/release.yml`) against `main`. The workflow only runs on
`workflow_dispatch`, and only after the full CI suite (lint, tests,
`govulncheck`, `gosec`) passes. When it runs it:

1. Resolves the new version from the latest `v*` git tag and the workflow
   inputs (the version lives in git tags, the Go idiom, not in a manifest file).
2. Builds, signs (cosign, keyless), and pushes the multi-arch image to GHCR,
   tagged with the version, `MAJOR.MINOR`, and `latest`, with an SBOM and build
   provenance, and Trivy-scans it (a fixable critical CVE fails the release).
3. Only after the image is built and clean, creates the `v<version>` git tag and
   cuts the `v<version>` GitHub Release using the notes you provide (see below),
   falling back to GitHub's auto-generated notes when you provide none.

The version is stamped into the binary via `-ldflags` (see
`internal/buildinfo` and the `Dockerfile`), so a running container reports its
version at startup and on the unauthenticated `/healthz` endpoint:

```sh
curl -s http://localhost:8080/healthz
# {"status":"ok","build":{"version":"1.2.0","commit":"abc1234","date":"2026-07-10T12:00:00Z"}}
```

## Cutting a release

From the Actions tab: **Actions -> Release -> Run workflow**, pick the branch
`main`, and fill in the inputs. Or from the CLI:

```sh
# Patch bump (default): 1.2.0 -> 1.2.1
gh workflow run Release --ref main

# Minor bump: 1.2.0 -> 1.3.0
gh workflow run Release --ref main -f release_type=minor

# Major bump: 1.2.0 -> 2.0.0
gh workflow run Release --ref main -f release_type=major

# Explicit version (overrides release_type)
gh workflow run Release --ref main -f version=1.3.0
```

With no tags yet, a `patch` bump produces `0.0.1`; pass `-f version=1.0.0` to
start the versioning wherever you like.

### Version inputs

| Input          | Effect                                                             |
| -------------- | ------------------------------------------------------------------ |
| `release_type` | `patch` (default), `minor`, or `major`. Bumps the latest `v*` tag. |
| `version`      | An explicit `MAJOR.MINOR.PATCH` string. Overrides `release_type`.   |

The workflow refuses to run if the resolved `v<version>` tag already exists, so
a release is never silently overwritten.

## Release notes

The notes are authored by hand and fed into the release. The workflow resolves
the release body in this order:

1. **`release_notes` input** - Markdown passed when you trigger the run. Best
   when triggering from the CLI so multi-line Markdown is preserved:

   ```sh
   gh workflow run Release --ref main -f release_type=minor \
     -f release_notes="$(cat my-notes.md)"
   ```

2. **`docs/release-notes/<version>.md`** - a committed file named for the exact
   version being released (for example, `docs/release-notes/1.3.0.md`). Commit
   it to `main` before triggering the run. Use this when you prefer the notes to
   live in the repo and go through review. A leading `# v<version>` heading in
   the file is stripped so the release body does not duplicate the title.

3. **GitHub auto-generated notes** - used only when neither of the above is
   provided, so a release never fails for lack of notes.

Whichever source wins becomes the full body of the GitHub Release.

## Continuous images vs releases

The **Docker** workflow (`.github/workflows/docker.yml`) is separate: it builds
on every push to `main` (moving `latest` plus a branch and short-SHA tag) and
builds without pushing on PRs. It deliberately does not run on tags, so it never
double-builds a release or races the signed release digest. Versioned,
cosign-signed `v<version>` images come only from the Release workflow.

## Archived release notes

Notes for past releases live alongside this file, one Markdown file per version
(`docs/release-notes/<version>.md`), mirroring the notes published on the
matching [GitHub Release](https://github.com/kenlasko/adguard-logcentral/releases).
No releases have been cut yet; the first one will add the first entry here.
