# rel

Publish a GitHub Release fully locally: goreleaser builds the artifacts,
packslip signs the manifest, gh uploads everything. No workflow.

## Adopting it

Two things. A `.goreleaser.yml` in the repo root, and a way to run this
command (`go run ./cmd/dev release ...`, a mise task):

```bash
go run ./cmd/dev release snapshot
# build release artifacts locally without publishing

go run ./cmd/dev release packslip --bin mytool --resource 'skill/mytool=repo:skills/mytool'
# build the packslip manifest for the snapshot artifacts and verify it
# (--resource is repeatable; omit it when the release ships no skill)

go run ./cmd/dev release publish --bin mytool --resource 'skill/mytool=repo:skills/mytool' --version v1.2.0
# tag, build, sign, upload. In CI, GITHUB_REF_NAME and GITHUB_SHA supply
# the tag and commit instead of --version and HEAD.
```

The repo slug comes from the origin remote, so no owner or repo name is
committed anywhere. The local signing key is ephemeral (a temp file), and
the manifest is unlogged (`--no-log`); CI signs with the workflow identity
instead.
