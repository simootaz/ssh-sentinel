# Development

How work on this repository is organised. The architecture and the frozen contract are in `architecture.md`; scope is in `roadmap.md`.

## Branches

Git flow: `feature/*` branches off `develop`, merged back into `develop`, `main` only for releases. Conventional commit messages (`feat(agent): ...`, `fix(backend): ...`, `docs: ...`).

## One worktree per session

Several sessions work on this repository at the same time, one per component or task. Each session must use its own git worktree. Never share one checkout between two sessions: two of them editing and committing in the same working directory mix their changes, stage each other's files and leave the index in a state neither of them expects.

Create a worktree per branch, next to the main checkout (PowerShell):

```powershell
cd "C:\project\git hub project\ssh-sentinel"
git worktree add ..\ssh-sentinel-backend feature/backend      # existing branch
git worktree add -b feature/mobile ..\ssh-sentinel-mobile develop   # new branch off develop
git worktree list
```

Rules:

- A session works, commits and pushes from its own worktree only. `git add`, `git commit` and `git push` run there, on that worktree's branch.
- A branch is checked out in one worktree at a time; git enforces it.
- Gitignored local files (`.env`, `android/local.properties`, `google-services.json`, personal working notes) are per worktree. Copy what the session needs; they are never committed.
- When the branch is merged, remove the worktree: `git worktree remove ..\ssh-sentinel-backend`. `git worktree prune` cleans up entries whose folder is gone.

Merges into `develop` happen from one place, after each component's own branch is pushed.
