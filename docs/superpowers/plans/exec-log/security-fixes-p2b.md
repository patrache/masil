# v0.1.3 security fixes — group p2b

Branch `fix/v013-sec-p2b` (from dev/v0.1.3). Findings (r) and (ae) of the commit security review.

## (r) logout / logout-all / suspension did not close WorkspaceHub sockets — fixed

- Was open: `server/src/services/users.ts` revokeSessions only called `closeDocSessions`; `server/src/do/workspace-hub.ts`
  had no session on `HubConn` and no revoke path, and `/connect` did not re-check the session.
- Fix: `HubConn.sessionId` (from X-Masil-Auth, now required); hub `POST /revoke {user_id, session_ids | "all", code}`
  closes matching sockets 4001 / 4003 and runs the departure (srv_peer left + left_at, or the owner's grace alarm);
  `/connect` re-reads the session in D1 after accepting and refuses a revoked one with 401;
  `services/workspace-shares.ts closeWorkspaceSessions` (live shares the user owns or is an active member of) runs in
  the background from revokeSessions next to closeDocSessions. `docs/contract/workspace.md` 4 and 8 list `/revoke`,
  4001 and 4003.
- Tests: `server/test/realtime/workspace-hub.test.ts` describe "WorkspaceHub: revoked sessions" (7 tests).

## (ae) join-request dialog steals focus — already fixed

- `internal/ui/workspace/owner.go` ownerEvent `share.EventJoinRequest` only queues the request (toast + bell + band);
  the check-number dialog opens only on ctrl+\ enter or enter in the people list (`nextJoinDialog`), fixed in
  cb0d56fb (P2 review). Test `TestJoinRequestWaitsForOwner` (review_fix_test.go) passes.
- Keyboard grant: `newKeyboardDialog` / `newExtendDialog` open only from the people list keys (people.go `k` / `K`),
  and granting needs the participant's handle typed (`TestKeyboardConfirmRequiresHandle`).
- Proposals: `onProposal` (proposals.go) opens nothing; the queue opens on an owner action and enter does not run a
  proposal (`TestProposalQueueEnterDoesNotRun`).
- Owner check-number dialog: the first selectable row is the level "viewer" (choosing it only sets the level); Allow
  is further down.

## 내 판단

- 내 판단: a suspended owner's live shares end at once (`endShare(…, "ended")`, members get 4100) instead of waiting
  out the 2-minute owner grace — a suspended account cannot come back within it, and the review asked for it. A plain
  logout of the owner keeps the grace (they may come back from another device).
- 내 판단: a hub socket with no recorded session (an attachment from before this change) counts as matching any revoke
  of that user — closing one too many is safe, the client redials with its live session.
- 내 판단: the Go share engine (`internal/workspace/share/handshake.go`, outside this group's area) treats 4001 / 4003
  like any unknown close and redials with backoff; the redial gets 401 and the app's UserHub `session.revoked` logs the
  user out anyway. Making 4001 / 4003 terminal in the engine (like collab.md 8 "재접속 금지") is left for the owner of
  that package.
- 내 판단: the participant's own check-number dialog (`dlg_join.go`, guest.go `EventSASReady`) still opens by itself
  with "match" first, but only while the participant is joining (before they have any pane to type into), right after
  their own join action — not the owner-steals-focus case of (ae); left as is.
