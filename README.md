# TrackPR

A small, aesthetic terminal UI for keeping an eye on the **open pull requests
you've authored** — across every git repository that lives under your home
folder.

- **Left pane** — your open PRs, grouped by repository. Within a group they are
  ordered by last update (oldest first); the groups themselves are ordered by
  their most recent activity, ascending.
- **Right pane** — a folder tree of the git repos (under your home directory)
  that currently have open PRs. Select any folder to filter the PR list to the
  repos beneath it. PR counts show as badges next to each folder/repo.

PRs **auto-refresh once a minute**, but the countdown resets on every keypress —
so a refresh never interrupts you mid-navigation. The list stays on screen
during a refresh; a small spinner next to the pane title (and in the footer) is
the only sign it's happening. Press `r` to refresh on demand.

## Requirements

- [`gh`](https://cli.github.com) — the GitHub CLI, **authenticated**
  (`gh auth status` should show you logged in). TrackPR shells out to
  `gh search prs --author=@me --state=open`.
- `git` on your `PATH`.
- Go 1.21+ to build.

## Build & run

```sh
go build -o trackpr .
./trackpr
```

Or run directly:

```sh
go run .
```

### Flags

| Flag     | Default | Description                                  |
|----------|---------|----------------------------------------------|
| `--root` | `$HOME` | Directory to scan recursively for git repos. |

```sh
./trackpr --root ~/work     # only scan repos under ~/work
```

## Keybindings

| Key            | Action                                              |
|----------------|-----------------------------------------------------|
| `Tab`          | Switch focus between the PR pane and the repo tree  |
| `↑` / `↓`, `k`/`j` | Move within the focused pane                    |
| `g` / `G`      | Jump to top / bottom                                |
| `→` / `←`, `l`/`h` | Expand / collapse a folder (tree pane)          |
| `Space`        | Toggle a folder open/closed (tree pane)             |
| `Enter`        | Tree: filter PRs to the selected folder · PR pane: open the PR in your browser |
| `Esc` / `Backspace` / `0` | Clear the filter (show all repos)        |
| `r`            | Refresh PRs from GitHub                             |
| `q` / `Ctrl-C` | Quit                                                |

## How it works

1. On startup it walks `--root` (default `$HOME`), recording every directory
   that contains a `.git`. It does **not** descend into a repo once found, and
   skips package caches, build output, and hidden/tooling directories
   (`node_modules`, `~/go/pkg`, `~/.nvm`, …).
2. For each repo it reads the `origin` remote and maps `github.com` URLs to an
   `owner/name`.
3. It asks `gh` for every open PR you authored, then keeps only those whose
   repository also exists locally under `--root`.
4. The tree filter matches on local path prefix, so a repo cloned in two places
   is reachable from either location.

## Layout / project files

| File         | Responsibility                                   |
|--------------|--------------------------------------------------|
| `main.go`    | Entry point, flags, bootstrap                    |
| `repos.go`   | Recursive repo discovery + remote parsing        |
| `github.go`  | Fetching open PRs via the `gh` CLI               |
| `tree.go`    | Folder-tree construction from repo paths         |
| `model.go`   | Bubble Tea model, update loop, navigation        |
| `view.go`    | Lipgloss styling and rendering                   |
| `view_test.go` | Layout/grouping/filter tests                   |
