# wb

Manage a pool of git repositories and the work that spans them.

- **Repo pool**: every repo is cloned once, at `<root>/<owner>/<repo>`.
- **Workbench**: one file in `<root>/.wb/workbenches/<name>.yaml` listing repos. A named, possibly
  overlapping view over the pool that sets defaults (repos offered, branch prefix).
- **Worktree**: a named group of git worktrees, one per repo, each on its own branch, under
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
wb new NAME [owner/repo...]              # create .wb/workbenches/NAME.yaml
wb clone [REPO...] [--all] [--add]       # clone missing repos (parallel)
wb sync [REPO...] [--all]                # fetch; fast-forward clean default branches

wb worktree create NAME                  # start here: empty worktree (folder + branch name), no repos yet
wb worktree add NAME [REPO[@BRANCH|@REMOTE:BRANCH]...] [--base REF] [--create]   # add repos as you decide on them (creates NAME if missing)
wb worktree list                         # dirty/ahead/behind per repo, untracked git worktrees
wb worktree path [NAME] [REPO]           # cd "$(wb worktree path NAME REPO)"
wb worktree remove [NAME] [REPO...] [--force]  # remove checkouts (branches are kept)
```

Global flags go anywhere on the command line: `wb -w microk8s worktree list`.

Worktree names can use conventional-commit style: `wb worktree create feat/add-rate-limiting`
creates branch name `feat/add-rate-limiting` (after the workbench's `branch_prefix`) and the flat folder
`.worktrees/feat-add-rate-limiting/`. Either spelling refers to the same worktree afterwards. Names
must not contain spaces (use dashes); `wb` rejects them.

You don't need to know the repos up front: create the worktree first, then decide which repos
belong to it and add each with `wb worktree add NAME REPO...` as you discover it. (`add` on a
missing worktree creates it, so `wb worktree add NAME api web` is a shortcut when you already know.)

### Continuing existing branches

A worktree is a named group (the folder); the branch is a property of each repo in it:

| Argument               | Branch                                                                                         |
|------------------------|------------------------------------------------------------------------------------------------|
| `REPO`                 | a new branch named after the worktree (after `branch_prefix`), from the default branch         |
| `REPO@BRANCH`          | exactly `BRANCH` (case kept, no prefix): the local branch, or one created from `origin/BRANCH` with that upstream, so `git push` updates the PR |
| `REPO@REMOTE:BRANCH`   | the same, taking `BRANCH` from the git remote `REMOTE` (e.g. a fork)                           |

```sh
wb worktree add fix/ingress api@Fix/Ingress-Timeout web@fix-ingress-ui lib
```

A `BRANCH` that exists neither locally nor on the remote is an error, so typos fail loudly; pass
`--create` to start it from the base instead. `wb` only runs git: look up branch names with `gh`
and add fork remotes yourself (`git -C <root>/owner/repo remote add NAME URL`). A repo already in
the worktree on another branch is refused. Each repo's branch is stored in the worktree state and
passed to hooks and `setup`.

| Flag / env                        | Meaning                                                           |
|-----------------------------------|-------------------------------------------------------------------|
| `-w`, `WB_WORKBENCH`              | Workbench to use (default: from the cwd, else picked)             |
| `-R`, `WB_ROOT`                   | Folder containing `.wb/` (default: found from the cwd)    |
| `WB_CLONE_PROTOCOL`               | `https` (default) or `ssh`, picks the GitHub clone URL            |
| `WB_CLONE_URL`                    | Clone URL template with `{owner}`/`{repo}`; overrides the protocol |

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
runs with `WB_SOURCE`, `WB_CHECKOUT`, `WB_REPO`, `WB_WORKBENCH`, `WB_WORKTREE` and `WB_BRANCH` (the repo's branch) set.

## Hooks

Hooks are shell commands (run with `sh -c`) that `wb` runs after a worktree changes. Declare them
under `hooks:` in either config file:

- user: `$XDG_CONFIG_HOME/wb/config.yaml` (default `~/.config/wb/config.yaml`)
- project: `<root>/.wb/config.yaml`

Both files are optional. The project value overrides the user value per hook; an empty project
value (`post_worktree_delete: ''`) switches a user hook off.

```yaml
hooks:
  post_worktree_create: 'code "$WB_WORKTREE_DIR"'
  post_worktree_repo_add: 'for r in $WB_REPOS; do echo "new: $r"; done'
  post_worktree_repo_remove: 'echo "dropped: $WB_REPOS"'
  post_worktree_delete: 'echo "deleted $WB_WORKTREE"'
```

| Hook                        | Runs                                                                                          |
|-----------------------------|-----------------------------------------------------------------------------------------------|
| `post_worktree_create`      | After the empty worktree folder and its state are created (`worktree create`, or `worktree add` on a missing worktree). |
| `post_worktree_repo_add`    | After the repos of one `worktree add` call are checked out and their `setup` ran. Once per call, and only if a repo was newly added. |
| `post_worktree_repo_remove` | After `worktree remove` took checkouts away. Once per call, for the repos removed.            |
| `post_worktree_delete`      | After the worktree itself is gone: removing its last repo, or removing an empty worktree. Runs after `post_worktree_repo_remove`. |

The working directory is the worktree root, `<root>/.worktrees/<slug>/`. Once that folder is
deleted (the last repo went, or `post_worktree_delete`), it is `<root>`.

Every hook gets these environment variables:

| Variable          | Value                                                                                       |
|-------------------|---------------------------------------------------------------------------------------------|
| `WB_HOOK`         | the hook name, e.g. `post_worktree_repo_add`                                                |
| `WB_ROOT`         | absolute root directory                                                                     |
| `WB_WORKBENCH`    | workbench name, empty if none                                                               |
| `WB_WORKTREE`     | worktree name, e.g. `feat/add-rate-limiting`                                                |
| `WB_WORKTREE_DIR` | absolute worktree root, e.g. `<root>/.worktrees/feat-add-rate-limiting`                     |
| `WB_BRANCH`       | the worktree's default branch for new repos, including the workbench `branch_prefix`; per-repo branches are in the JSON `repos[].branch` |
| `WB_REPOS`        | space-separated `owner/repo` names affected by this call: newly added (`post_worktree_repo_add`), removed (`post_worktree_repo_remove`), empty for `post_worktree_create` and `post_worktree_delete` |

The same data arrives as JSON on stdin, with the structured details of each repo:

```json
{
  "hook": "post_worktree_repo_add",
  "root": "/abs/root",
  "workbench": "platform",
  "worktree": "feat/add-rate-limiting",
  "worktree_dir": "/abs/root/.worktrees/feat-add-rate-limiting",
  "branch": "dev/feat/add-rate-limiting",
  "repos": [
    {"name": "acme/api", "checkout": "/abs/root/.worktrees/feat-add-rate-limiting/api", "base": "origin/main", "branch": "dev/feat/add-rate-limiting", "created": true}
  ]
}
```

`repos` is `[]` for `post_worktree_create` and `post_worktree_delete`. For `post_worktree_repo_add`
it also lists repos of the call that were already present, with `"created": false` (they are not
in `WB_REPOS`). For `post_worktree_repo_remove` it lists the removed repos with `"created": false`
and `checkout` set to the removed path.

Hooks run after the state is written. A failing hook prints its error and makes `wb` exit 1, but
checkouts and removals are never rolled back. A config file with unknown keys or hooks is rejected
before anything changes. The per-repo `setup` in the workbench file is separate and unchanged: it
runs in each new checkout, before `post_worktree_repo_add`.

## Layout

```
main.go                 calls cmd.Execute()
cmd/                    cobra commands: root.go, then one file per command
internal/{config,pool,manifest,worktree,hooks,root,names}/   logic
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

Dependabot (`.github/dependabot.yml`) opens weekly updates for Go modules and GitHub Actions.
`.github/workflows/dependabot-automerge.yml` turns on auto-merge for its patch and minor updates, so
they merge once the required checks pass; major updates are left for review. For that to wait for
CI, enable **Allow auto-merge** in the repository settings and add a branch protection rule (or
ruleset) for `main` that requires the `test (ubuntu-latest)`, `test (macos-latest)`, `lint`,
`go mod tidy` and `release dry run` checks.
Without required checks GitHub merges immediately.

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
