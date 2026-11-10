# wb

Manage a pool of git repositories and the work that spans them.

- **Repo pool**: every repo is cloned once, at `<root>/<owner>/<repo>`.
- **Workbench**: one file in `<root>/workbenches/<name>.yaml` listing repos. A named, possibly
  overlapping view over the pool that sets defaults (repos offered, branch prefix).
- **Worktree**: a named set of git worktrees on one shared branch, one per repo, under
  `<root>/.worktrees/<name>/`. It can include any pool repo, not only the workbench's.

## Install

```sh
go install github.com/canonical/wb@latest
```

Or download a binary (linux/macOS, amd64/arm64) from the GitHub releases page and verify it against
`checksums.txt`. Or from a checkout: `go install .`. `wb --version` prints the version. `git` is
required; `fzf` is optional (without it,
pickers fall back to numbered prompts).

## Usage

```sh
wb list                                  # workbenches, cloned/total, open worktrees
wb new NAME [owner/repo...]              # create workbenches/NAME.yaml
wb clone [REPO...] [--all] [--add]       # clone missing repos (parallel)
wb sync [REPO...] [--all]                # fetch; fast-forward clean default branches

wb worktree add NAME [REPO...] [--base REF]   # create or grow a worktree set, e.g. feat/add-rate-limiting
wb worktree list                         # dirty/ahead/behind per repo, untracked git worktrees
wb worktree path [NAME] [REPO]           # cd "$(wb worktree path NAME REPO)"
wb worktree remove [NAME] [REPO...] [--force]  # remove checkouts (branches are kept)
```

Global flags go anywhere on the command line: `wb -w microk8s worktree list`.

Worktree names can use conventional-commit style: `wb worktree add feat/add-rate-limiting api web`
creates branch `feat/add-rate-limiting` (after the workbench's `branch_prefix`) in the flat folder
`.worktrees/feat-add-rate-limiting/`. Either spelling refers to the same worktree afterwards. Names
must not contain spaces (use dashes); `wb` rejects them.

| Flag / env                        | Meaning                                                           |
|-----------------------------------|-------------------------------------------------------------------|
| `-w`, `WB_WORKBENCH`              | Workbench to use (default: from the cwd, else picked)             |
| `-R`, `WB_ROOT`                   | Folder containing `workbenches/` (default: found from the cwd)    |
| `WB_CLONE_URL`                    | Clone URL template, default `git@github.com:{owner}/{repo}.git`   |

## Workbench file

```yaml
description: Canonical Kubernetes
branch_prefix: ""                # optional, e.g. "<user>/"; prepended to the branch name
repositories:
  - canonical/k8s-snap
  - repo: canonical/k8s-operator
    default_branch: main         # optional; default: origin/HEAD
    setup: "make deps"           # optional; runs in each new worktree checkout
```

Per-repo settings are merged across workbenches; conflicting values are a config error. Setup
runs with `WB_SOURCE`, `WB_CHECKOUT`, `WB_REPO`, `WB_WORKBENCH` and `WB_WORKTREE` set.

## Layout

```
main.go                 calls cmd.Execute()
cmd/                    cobra commands: root.go, then one file per command
internal/{config,pool,manifest,worktree,root,names}/   logic
internal/{gitx,pick,parallel}/                         helpers (git, fzf/prompt, concurrency)
```

This is the layout `cobra-cli` generates. A new command is a new file in `cmd/` that declares a
`var fooCmd = &cobra.Command{...}` and calls `rootCmd.AddCommand(fooCmd)` (or
`worktreeCmd.AddCommand` for a subcommand) from its `init()`.

## Development

```sh
go test ./...
go vet ./...
golangci-lint run        # same linter as CI (config in .golangci.yml)
```

CI (`.github/workflows/ci.yml`) runs vet and tests on Linux and macOS, lint, a `go mod tidy` check
and a GoReleaser snapshot build on every pull request and push to `main`.

## Releasing

Push a semver tag; `.github/workflows/release.yml` runs the tests and then GoReleaser, which builds
linux/darwin × amd64/arm64 archives with `checksums.txt` and publishes a GitHub release with a
changelog grouped by conventional-commit type (`feat:`, `fix:`).

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Try a release locally without publishing: `goreleaser release --snapshot --clean --skip=publish`.

## Agent skill

[.github/skills/wb/SKILL.md](.github/skills/wb/SKILL.md) teaches coding agents how to use `wb`
(non-interactive usage, safe removal, workflows, troubleshooting).

## License

[Apache License 2.0](LICENSE).
