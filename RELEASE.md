# Release process

How to cut a new release of `all-bench`.

## How it works

- Builds and publishing are automated by [GoReleaser](https://goreleaser.com) via [.github/workflows/release.yml](.github/workflows/release.yml).
- The workflow triggers when a git tag matching `v*` is pushed (e.g. `v0.2.0`).
- On trigger, GoReleaser builds binaries for macOS/Linux/Windows (amd64/arm64), and creates a GitHub Release with notes auto-generated from commit messages (grouped into Features/Bug Fixes/Documentation/Other — see `.goreleaser.yaml`).
- `CHANGELOG.md` is maintained by hand in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. It's the human-curated record; the GitHub Release notes are a separate, auto-generated summary from commits.

## Checklist for each release

1. **Working tree is clean.**
   ```
   git status
   ```
   Should print "nothing to commit, working tree clean". Commit or stash anything outstanding first.

2. **Every change since the last tag is documented.** List what happened since the last release:
   ```
   git log $(git describe --tags --abbrev=0)..HEAD --oneline
   ```
   For each user-facing commit, make sure there's a line under `## [Unreleased]` in `CHANGELOG.md` (Added / Changed / Fixed / Removed). Internal-only changes (e.g. a `todo` file, CI tweaks) don't need an entry.

3. **Pick the next version** (semver):
   - `patch` (0.1.x) — bug fixes only
   - `minor` (0.x.0) — new features, backwards compatible
   - `major` (x.0.0) — breaking changes

4. **Cut the CHANGELOG entry.** Move everything currently under `## [Unreleased]` into a new dated section above the previous release, e.g.:
   ```markdown
   ## [Unreleased]

   ## [0.2.0] - 2026-07-23

   ### Added
   - ...
   ```
   Leave `## [Unreleased]` empty at the top for the next round.

5. **Commit the changelog:**
   ```
   git add CHANGELOG.md
   git commit -m "chore: release v0.2.0"
   git push origin main
   ```

6. **Tag and push the tag** — this is what triggers the release build:
   ```
   git tag v0.2.0
   git push origin v0.2.0
   ```

7. **Watch the build:** https://github.com/RashRAJ/all-bench/actions

8. **Verify the published release** (binaries attached, notes look right): https://github.com/RashRAJ/all-bench/releases

## Commit message convention

GoReleaser groups the auto-generated GitHub release notes by prefix, so use these when the commit is user-facing:

| Prefix | Shows up as |
|---|---|
| `feat: ...` | Features |
| `fix: ...` | Bug Fixes |
| `docs: ...` | Documentation |
| anything else | Other |
| `test: ...` / `chore: ...` | excluded from the auto-generated notes entirely |
