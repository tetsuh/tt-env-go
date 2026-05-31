# Agent workflow guide

This file is the canonical workflow guide for coding agents working in this
repository. Tool-specific instruction files should stay short and point back to
this file instead of duplicating the full workflow.

## Repository-wide default rules

- Use GitHub Flow.
- Branch from the latest `main` unless the user specifies another base.
- Keep changes scoped to the requested issue or task.
- Follow the branch naming, commit message, PR, validation, and AI review
  conventions below.
- Do not revert unrelated user changes.
- Do not delete untracked local files unless explicitly requested.

## Session-specific operational choices

Operational choices such as whether to merge, create draft PRs, run work in
parallel, wait for AI reviews, split issues into parent/child issues, or perform
manual hardware validation should follow the user's instructions for the current
session.

## Branch naming

- Use `feature/<issue-number>-<short-slug>` for feature or work branches.
- Do not use the `feat/` branch prefix.
- Example: `feature/2-initialize-go-module`.

## Issue creation

- Create GitHub Issues when work needs tracking, discussion, or a PR reference.
- For multi-step work, use GitHub Issues parent/child relationships when
  available and when they improve tracking; otherwise use Task Lists or
  explicit parent issue references.
- Include these sections where useful:
  - Background
  - Goal
  - Scope
  - Out of scope
  - Acceptance criteria
  - Validation plan
  - Notes / risks
  - Related issues / PRs
- Keep issue scope actionable and avoid mixing unrelated tasks.

## Commit messages and PR titles

- Use Conventional Commits for commit messages and PR titles.
- When a commit message has a body, write body details as bullet points starting
  on line 3, without blank lines between bullet items.
- Examples:
  - `feat(cli): add status command`
  - `fix(manifest): handle missing os-release fields`
  - `docs(workflow): document agent instructions`
- Include the appropriate co-author trailer when creating commits. Use generic, non-ID email addresses to avoid creating links to specific tool-provider bot or user accounts:
  - For Copilot: `Co-authored-by: Copilot <noreply@github.com>`
  - For Antigravity: `Co-authored-by: Antigravity <noreply@google.com>`
  - For Gemini CLI: `Co-authored-by: Gemini CLI <noreply@google.com>`
  - For Claude: `Co-authored-by: Claude <noreply@anthropic.com>`

## PR body

- Include a concise summary.
- Include validation performed.
- Link the relevant issue with `Refs #<issue>`, `Closes #<issue>`, or
  `Fixes #<issue>` as appropriate.

## AI review workflow

- Check Gemini and GitHub Copilot review feedback after opening or updating PRs
  when AI review is expected.
- Poll every 1 minute for up to 8 minutes after initial PR creation.
- After pushing fixes, explicitly request rereview before polling again; pushing
  alone does not trigger AI rereview.
- Poll every 1 minute for up to 8 minutes after rereview requests.
- Check review comments, submitted reviews, and issue comments.
- Treat Gemini authors as either:
  - `gemini-code-assist`
  - `gemini-code-assist[bot]`
- Treat GitHub Copilot review authors as:
  - `copilot-pull-request-reviewer` for submitted review summaries
  - `Copilot`
- For GitHub Copilot reviews, check both submitted reviews and inline PR review
  comments. Inline comments can appear from `Copilot` even when the review
  summary author is `copilot-pull-request-reviewer`.
- For Gemini rereview, reply to the specific review comment thread with:
  - `/gemini review`
- For GitHub Copilot review comments, address required changes, request a new
  Copilot review using the PR's GitHub-supported Copilot review trigger, then
  poll for the response.
- Resolve review threads only after the AI reviewer indicates the issue is
  OK/resolved.

## Merge method and squash commit message

- Use squash merge as the default merge method.
- Use a Conventional Commit style squash commit title.
- Put details in the squash commit body starting on line 3.
- Use bullet points for the body details, without blank lines between bullet
  items.
- Example:

```text
docs(workflow): add agent instructions

- Add AGENTS.md as the canonical agent workflow guide.
- Add Copilot and Gemini entry points.
- Document branch, issue, review, and validation rules.

Co-authored-by: Copilot <noreply@github.com>
```

## Validation

- Use the standard Go toolchain to validate changes:
  - `gofmt -l .` (must report no files)
  - `go vet ./...`
  - `go build ./...`
  - `go test ./...`
- Run the full set before opening a pull request so failures can be fixed before
  review.
- If a required toolchain component is unavailable locally, note the limitation
  in the PR and rely on CI for that check.
- For KMD, system package, or hardware-dependent changes, document any required
  manual validation on real hardware when applicable.
- Do not add new lint/test tools unless required for the task.

## Safety and repository hygiene

- Preserve unrelated changes in the working tree.
- Avoid destructive git commands unless explicitly requested.
- Keep documentation changes consistent with `README.md` and existing workflow
  docs.
- Do not commit local-only planning or session handoff documents (for example
  `plan.md`, `SESSION.md`, `HANDOFF.md`); they are ignored via `.gitignore`.
