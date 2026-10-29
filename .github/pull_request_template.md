<!-- Every PR links the issue or notice it serves. Closes #N only if this PR finishes an issue in this repo; otherwise Refs #N.
     Fleet work from AI Operations: Refs SchoolAI/sai-internal-apps#N -->
Refs #

## What changed, and for whom

<!-- One to three lines a non-engineer can read: what someone using the app will notice. -->

## How it was checked

<!-- Paste each command and its result line. "It works" always carries the command. -->
- App's own checks: `make test` → <result>
- Fleet check, from sai-internal-apps: `npm run apps -- check slack-mcp-server` → <result line>

## Writes outside this PR

<!-- None, or: system · dry run · digest · approval link · undo (sai-internal-apps skills/apps-controls).
     Deploys, variables, hosts, access lists and secrets never ride along in a PR. -->
None.

## Checklist

- [ ] Links an issue or notice above
- [ ] No `.env`, tokens, user or customer data, or personal data committed
- [ ] README and AGENTS.md updated if behaviour or setup changed
- [ ] Reviewed by a code owner who didn't write it
