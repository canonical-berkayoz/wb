---
name: wb
description: 'Use the `wb` CLI to manage a pool of git repositories, workbenches (one YAML file per repo set) and multi-repo worktrees. Use when asked to clone or sync a set of repos, start or finish any change you are making or introducing (feature, fix, bump, upgrade, refactor, experiment) in one or more repositories, create same-branch git worktrees across repos, list worktree status (dirty/ahead/behind), find a worktree path, or define a workbench file. Triggers: wb, workbench, worktree, multi-repo, repo pool, cross-repo change, .worktrees.'
---

# wb: repo pool, workbenches, multi-repo worktrees

## Concepts

- **Pool**: every repo is cloned once, at `<root>/<owner>/<repo>`. Never clone repos by hand into the root; use `wb clone`.
- **Workbench**: one file `<root>/workbenches/<name>.yaml` listing repos. A named, possibly overlapping view over the pool. It decides which repos `clone`/`sync` act on by default and sets the branch prefix.
- **Worktree**: a named set of git worktrees on one shared branch, one per repo, at `<root>/.worktrees/<name>/<repo>/`. It may include any pool repo, not only the workbench's. State lives in `<root>/.worktrees/<name>/.wb-worktree.json`; do not edit it by hand.
- **Root**: the folder containing `workbenches/`. Found from `-R/--root`, `$WB_ROOT`, or by walking up from the cwd.

## Rules for agents (read first)

1. **Never trigger an interactive prompt.** `wb` prompts on stdin when an argument is missing, which hangs a non-interactive session. Always pass:
   - the worktree name and the repos to `worktree add`,
   - the worktree name to `worktree path` and `worktree remove`,
   - `-w WORKBENCH` whenever the cwd is not inside a worktree or a pool repo and there is more than one workbench.
2. **Discover before acting**: run `wb list` and `wb worktree list` first. They are read-only.
3. **Prefer `wb worktree path NAME REPO`** to locate a checkout, then work in that directory. Do not guess paths.
4. **Never pass `--force` to `worktree remove` unless the user asked**: it discards uncommitted changes. The default refusal is the safety net; report what was kept instead.
5. **Branches are never deleted by `wb`.** Do not delete them either unless asked.
6. Do not edit the root repo's `.gitignore`; `wb` keeps pool folders ignored itself.
7. Exit code `0` means everything succeeded; `1` means a failure that was already printed (`Error: …`, or `✗ …` lines). Partial failures exit `1` and keep going for the other repos, so read the whole output.

## Commands

Global flags (accepted anywhere): `-w/--workbench NAME` (env `WB_WORKBENCH`), `-R/--root DIR` (env `WB_ROOT`).

| Command | Purpose |
|---|---|
| `wb list` | Workbenches with `cloned/total` repos and open worktree counts. |
| `wb new NAME [owner/repo...] [--force]` | Create `workbenches/NAME.yaml` from a commented template. |
| `wb clone [REPO...] [--all] [--add]` | Clone missing repos in parallel. Default: the workbench's repos. `--all`: whole pool. `--add`: allow `owner/repo` not in any workbench. |
| `wb sync [REPO...] [--all]` | `git fetch --prune`; fast-forward the default branch only when clean and not diverged. |
| `wb worktree add NAME [REPO...] [--base REF] [--add]` | Create the worktree set on branch `<branch_prefix>NAME`, or grow an existing one. |
| `wb worktree list` | Per-repo `dirty N` / `clean`, `↑ahead ↓behind vs <base>`, plus "untracked worktrees" not created by wb. With `-w`, only that workbench's worktrees. |
| `wb worktree path NAME [REPO]` | Print the worktree folder, or one checkout. |
| `wb worktree remove NAME [REPO...] [--force]` | Remove checkouts; dirty ones are kept unless `--force`. Branch is kept. |

`REPO` is `owner/repo`, or just `repo` when the name is unique in the pool. If two repos share a name, use `owner/repo`; inside a worktree they get folders `<owner>-<repo>`.

`NAME` follows conventional-commit style: `<type>/<short-kebab-description>`, for example `feat/add-rate-limiting`. The branch is `<branch_prefix><NAME>` with the slash kept (`feat/add-rate-limiting`), and the folder is flat (`.worktrees/feat-add-rate-limiting/`). Anywhere a worktree `NAME` is accepted, `feat/add-rate-limiting` and `feat-add-rate-limiting` refer to the same worktree. Names must not contain spaces (`wb` rejects them); use lowercase kebab-case, e.g. `fix/dns-timeout`, never `"fix dns timeout"`. Uppercase is lowercased and other characters outside `a-z 0-9 . _ -` become `-`.

## Workflows

### Start any change

Every change you make or introduce gets its own worktree: one branch, a checkout per repo it touches. Always name it `<type>/<description>` with a conventional-commit type:

| Type | Use for |
|---|---|
| `feat/` | New capability |
| `fix/` | Bug fix |
| `chore/` | Maintenance, dependency bumps, tooling |
| `refactor/` | Restructuring without behavior change |
| `docs/`, `test/`, `ci/`, `build/`, `perf/` | As in conventional commits |

Pick the type that matches the intent, and keep the description short and specific (`fix/dns-timeout`, `chore/bump-cilium`). If the user already named the change, keep their words and add the type.

```sh
wb list
wb -w platform clone                                      # ensure the pool has what the workbench lists
wb -w platform worktree add feat/add-rate-limiting api web  # one branch, two checkouts
cd "$(wb worktree path feat/add-rate-limiting api)"       # work here; commit with plain git
```

Add another repo to the same change (even from another workbench): `wb worktree add feat/add-rate-limiting shared/lib`. Re-running `add` with no repos re-checks the existing set and is idempotent (`= repo (already present)`).

### Check progress

```sh
wb worktree list
```

`↓N` means the base moved since the branch was created; rebase or merge in that checkout with git.

### Refresh the pool

```sh
wb -w platform sync
```

Main checkouts are only fast-forwarded when clean. Notes such as `not fast-forwarded: working tree is dirty` are informational; do not "fix" them by resetting the user's work.

### Finish

```sh
wb worktree remove feat/add-rate-limiting
```

If it prints `✗ <repo> kept: … contains modified or untracked files`, commit/push or ask the user before using `--force`. Push branches with plain git from the checkout before removing; `wb` never pushes.

### Define a workbench

```sh
wb new platform acme/api acme/web
```

Then edit `workbenches/platform.yaml`:

```yaml
description: Platform services        # shown by `wb list`
branch_prefix: "dev/"                  # optional; prepended to the branch name (dev/feat/add-rate-limiting)
repositories:
  - acme/api
  - repo: acme/web
    default_branch: main               # optional; default: origin/HEAD
    setup: "make deps"                 # optional; runs in each NEW worktree checkout
```

Only `repositories` is required. Per-repo settings are merged across workbenches; the same repo with different `default_branch` or `setup` in two files is a config error naming both files. Unknown keys are errors.

`setup` runs with cwd = the new checkout and env `WB_SOURCE`, `WB_CHECKOUT`, `WB_REPO`, `WB_WORKBENCH`, `WB_WORKTREE`. A failing setup prints the last 15 lines, exits `1`, and does not stop other repos.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `could not find a root …` | Run from inside the root, or pass `-R DIR` / set `WB_ROOT`. |
| `unknown workbench "x" (known: …)` | Check `wb list`; names are file names without `.yaml`. |
| `x is not in the pool; pass --add …` | The repo is in no workbench and not cloned. Use `--add` (clone) / `--add` (worktree add) only if the user wants it. |
| `"x" is ambiguous …` | Two pool repos share a name; use `owner/repo`. |
| `invalid workbench configuration: …` | Every problem is listed at once; fix the YAML files named. |
| `cannot determine the default branch; set default_branch` | `origin/HEAD` is unset for that repo; add `default_branch:` in the workbench file. |
| Clone fails with permission/host-key errors | Clones use SSH (`git@github.com:{owner}/{repo}.git`). Fix SSH access, or set `WB_CLONE_URL` to a template with `{owner}` and `{repo}` (for example an HTTPS URL). |
| Command waits for input | You omitted an argument that triggers a picker. Interrupt and re-run with explicit arguments. |

## Install

`go install github.com/canonical/wb@latest` (needs `git`; `fzf` is optional and only improves interactive pickers).
