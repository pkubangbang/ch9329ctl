# PUBLISH — CH9329CTL 1.0.0

Target: **https://github.com/pkubangbang/ch9329ctl** (public)
Source tree: `D:\proj\news\_scratch-ch9329-repo` — branch `main`, 29 files staged, 0 commits.

Verified before staging: `go build ./...` OK on windows / linux / darwin, `go vet` clean,
`go test` ok, `gofmt -l .` clean, all 18 documented commands present, `selftest` PASS.

Pre-flight for the push itself:
- `gh auth status` → logged in as **pkubangbang**, token scopes `gist, read:org, repo, workflow`.
  The `workflow` scope is required to push `.github/workflows/ci.yml` — it is present.
- The repo name is free (`gh repo view pkubangbang/ch9329ctl` → "Could not resolve to a
  Repository"), so `gh repo create` will not collide.

If a *different* machine or token is used and the push is rejected with
`refusing to allow an OAuth App to create or update workflow`, grant the scope first:

```
gh auth refresh -s workflow,repo
```

---

## Fastest path — let the agent finish it

1. Press **ESC** to leave auto mode.
2. Say **`go`**.

The agent then runs, in one batch:

```
git_commit (cwd=_scratch-ch9329-repo, message="CH9329CTL 1.0.0 - CH9329 USB-HID bridge driver")
gh repo create ch9329ctl --public --source=. --remote=origin --push
```

and reports the resulting URL.

The gate is coherent and unavoidable from inside the agent: `git_commit` is
auto-rejected while auto mode is ON ("the user was not asked"), `bash git commit`
is redirected back to the same tool, and `gh repo create --push` refuses with
"no commits found". Only the user exiting auto mode releases it.

---

## Or do it yourself — from the staged tree

```
cd D:/proj/news/_scratch-ch9329-repo
git commit -m "CH9329CTL 1.0.0 - CH9329 USB-HID bridge driver"
gh repo create ch9329ctl --public --source=. --remote=origin --push
```

### Or from the packaged archive (portable, includes `.git`)

```
tar -xzf ch9329ctl-repo-staged.tar.gz -C ch9329ctl
cd ch9329ctl
git commit -m "CH9329CTL 1.0.0 - CH9329 USB-HID bridge driver"
gh repo create ch9329ctl --public --source=. --remote=origin --push
```

Archive: `.mycc/public/ch9329ctl-repo-staged.tar.gz`. Use `tar`, not
`Compress-Archive` — the latter silently drops hidden directories such as `.git`.

### Or wire the remote explicitly (create the empty repo on github.com first)

```
git -C D:/proj/news/_scratch-ch9329-repo commit -m "CH9329CTL 1.0.0 - CH9329 USB-HID bridge driver"
git -C D:/proj/news/_scratch-ch9329-repo remote add origin https://github.com/pkubangbang/ch9329ctl.git
git -C D:/proj/news/_scratch-ch9329-repo push -u origin main
```

---

## After publish

```
gh repo view pkubangbang/ch9329ctl --web
gh run list --repo pkubangbang/ch9329ctl     # CI cross-compiles windows/linux/darwin + vet
```
