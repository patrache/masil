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

## Round 2 (reviewer follow-ups on 7f247570)

### (r) follow-up: the Go share engine redialed after 4001 / 4003 — fixed (d9a19864)

- Was open: `internal/workspace/share/handshake.go` `connGone` handled 4000/4008/4100/4101/4102 only; 4001 and 4003
  fell to the default branch (EventReconnecting + backoff redial), and `redial` stopped only on a 404, so a revoked
  member redialed into 401 until the app tore the engine down. This supersedes the third "내 판단" bullet above.
- Fix: `share.CloseUnauthorized` (4001) / `share.CloseSuspended` (4003); `connGone` ends the share with
  `ReasonUnauthorized` / `ReasonSuspended`; `redial` treats `401` and `403 account_suspended` as terminal like the 404.
  `sharetest.Relay.Revoke(user, code)` mirrors the hub's `POST /revoke` + requireAuth (later dials 401 / 403; a
  suspended owner's share ends).
- Tests: `internal/workspace/share/revoke_test.go` `TestRevokeCloseIsTerminal` (guest and host, 4001 and 4003: EventEnded
  with the reason, no EventReconnecting, no dial for a minute after), `TestRedialAuthRefusalIsTerminal` (401 / 403 on a
  redial after a network drop).
- 내 판단: the UI gets no new text. The participant screen shows `공유가 끝났어요` for any EventEnded (guest.go), and
  the owner screen goes back to solo; the login screen / suspension notice itself comes from the app's realtime
  client (events.md 4001 / 4003, `ui/app/bridge.go`), which receives the same revocation.

### (ae) follow-up: the tamper warning dialog opened by itself with "stop" selected — fixed

- Was open: `internal/ui/workspace/owner.go` ownerEvent `share.EventTamperWarning` set `s.dlg = tamperDialog(s)` at
  once with `TamperStop` (host.Kill) as the selected first option, so an enter the owner was typing in a terminal
  stopped the share.
- Fix: `form.holdUntil` — a dialog that opened by itself drops every key for `selfOpenHold` (700 ms); the tamper
  dialog uses it and starts on `TamperContinue` (`sel: 1`); the option order and texts are unchanged, so no golden
  changed.
- Test: `internal/ui/workspace/tamper_dialog_test.go` `TestTamperDialogIgnoresTypedKeys` ("l s enter up enter" right
  after the warning neither stops the share nor closes it; after 1 s enter continues; up enter still stops).
- 내 판단: the dialog still opens by itself rather than waiting in the band like a join request, because spec B9 says
  to warn the owner and ask whether to stop, and an active tampering alert should not wait for the owner to look. The
  hold + safe default removes the accidental stop. The MITM notice (`noticeDialog`, only a Cancel option) is left
  opening by itself: its only answer closes it, so stray keys cannot do harm.
