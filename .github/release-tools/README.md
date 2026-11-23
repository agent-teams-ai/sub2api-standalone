# Release matrix

The release workflow builds the frontend once, then runs each configured Go target on its own Linux runner. `CGO_ENABLED=0` permits cross-compilation. The target matrix is read from `.goreleaser.yaml`, including its exclusions; simple releases select Linux amd64 only.

Each build uses GoReleaser OSS in snapshot mode with the selected release version and one target. Archive naming, bundled files, Go flags and release templates remain in the existing GoReleaser configurations. Every archive is accompanied by its source commit, target, version and SHA256. The publishing job verifies the complete matrix before building images or publishing. It uses GoReleaser's `extra_files` support to publish existing archives and checksums, with builds disabled. No Pro license is needed.

Go caches are isolated by target and refreshed on each source commit, with fallback to the preceding target cache. Save uses the original restore key, even if a build hook changes `go.sum`. Matrix jobs upload uniquely named artifacts. The publishing job extracts only the regular Linux binary from each verified archive and restores its executable permission before constructing Docker contexts. QEMU remains limited to runtime-image instructions. DockerHub images are omitted when its credentials are absent; GHCR is always retained. Simple mode still publishes only the amd64 GHCR image and the simple release description.

All build jobs use the commit resolved by `prepare`, including a manual release's selected tag. Helper scripts come from the workflow revision and are passed as a run-local artifact, so older application tags do not need to contain the new scripts. The workflow serializes release runs to prevent simultaneous updates to moving image tags.

## Validate without publication

From a branch containing this workflow:

```bash
gh workflow run release.yml --ref <branch> \
  -f tag=<branch> -f dry_run=true -f simple_release=false
```

A dry run builds all selected archives and both runtime images, verifies artifact provenance and produces the final checksum file. It exports images locally as OCI archives instead of pushing them. It skips registry logins, GitHub Release publication, DockerHub description updates, Telegram notifications and VERSION synchronization. Test the simple path separately with `simple_release=true`.

Dry-run artifacts are available in the Actions run, including `release-dry-run-report`. Compare job start/end times, GoReleaser's build duration and cache restore results. Do not present an initial cold-cache run as a warmed-cache benchmark; publishing network time is not measured by dry runs.

Helper checks:

```bash
python -m pip install -r .github/release-tools/requirements-release.txt
python -m unittest discover -s .github/release-tools -p 'test_release_matrix.py'
bash -n .github/release-tools/release-images.sh
```

## Historical backend in the database-upgrade smoke

The owning CI still builds the complete head release image and checks its fresh
installation and upgraded installation, including UI, login, migrations and
clean shutdown logs. The historical comparator only creates the old database,
admin and persisted configuration before that upgrade. It does not qualify an
old frontend, a complete old release image, or the separate declared rollback
image for new operation encodings.

`upgrade_comparator.py` builds the immutable base commit with its existing
nonembedded `backend/Makefile` target and exact Go pin. It records the source
revision, release linker fields and Go build information, and refuses dirty or
extra build inputs. It reuses the already-built head image by immutable image ID
only when the inherited runtime stages, backend resource preparation, relevant
global arguments, Docker context filters, entrypoint and every copied runtime
input match the base. The historical binary is mounted read-only at
`/app/sub2api`; the normal entrypoint, user, environment and upgrade volume are
retained. Any mismatch fails the smoke without rebuilding an old frontend.

The receipt becomes complete only after the existing upgraded UI, login,
migration and log checks pass. CI retains that receipt and binary/version
provenance. The binary itself is a run-local artifact, not a published release.

Run the bounded source/provenance guards with:

```sh
python -m unittest discover -s .github/release-tools -p 'test_upgrade_comparator.py'
```
