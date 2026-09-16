# release

`dev release DIR [VERSION]` publishes a GitHub Release of one command
directory: goreleaser builds it for linux, darwin and windows on amd64 and
arm64, packslip signs the manifest, gh uploads. `--snapshot` builds, signs with
a throwaway key and verifies, publishing nothing.

Nothing is configured per repo. The binary is named after the repo (or
`--name`), every directory under `skills/` ships as a skill, and the goreleaser
config is generated unless the repo keeps a `.goreleaser.yml` of its own. The
same command runs locally, where VERSION tags and pushes, and in CI, where the
pushed tag is the version and packslip signs with the workflow's identity.
