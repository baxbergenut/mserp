# Feedback

`/feedback` is shared by every signed-in Clustie user and appears immediately
above Settings. No extra role permission is required. Feed is the default view;
Kanban shows Open, In progress, Completed and Discarded in separate paginated
columns. Discarded requests are excluded from the feed and its status filter;
they remain available in Kanban. The Share feedback action sits at the right of
the view/search/filter toolbar. Search, tag filtering and newest/most-upvoted sorting run on the server.
The visible feed refreshes every 30 seconds and when returning to the tab.

Everyone can post, comment, add tags to their own posts, and upvote/unvote once
per post. Authors can edit their title, details and tags. The built-in
Administrator can edit any post and change statuses through the detail selector
or Kanban drag and drop. Status changes retain an append-only author/time history.
The `access.manage` permission alone does not grant moderation.

On each transition into Completed, migration 157 and `feedback_messages.go`
save a private completion message for the original author in the same transaction
as the status/history change. Tasks shows these messages with a View feedback
link and Mark as read. Unread messages count toward the Tasks sidebar badge;
visible pages refresh within 30 seconds and on focus. Read messages remain in
history. Re-saving Completed or retrying a stale save never duplicates a message.
Reopening and completing again creates a new dated update. Historical completions
are not backfilled. No email is sent.

Every signed-in user can open Tasks for their personal feedback messages.
Operational tasks and their API permissions/privacy are unchanged; users without
`tasks.read` never fetch or see that board. Even Administrators cannot read or
acknowledge another user's personal messages through these endpoints.

The composer requests detailed reproduction steps, expected/actual results,
affected pages, impact and examples. It accepts file selection, drag/drop and
pasted clipboard screenshots, showing previews before submission. Images appear
as post media with the description beneath as a caption; non-image files retain
download links. Attachments
are included atomically in post creation: up to five files, 10 MB each, 20 MB
combined. Existing attachments remain unchanged when editing text. Every signed-in
user can view/download attachments through Feedback's own authenticated endpoint;
there are no public file URLs. Content sniffing permits inline PNG/JPEG/WEBP/GIF
only; other types download with `nosniff` and a sandbox CSP. Files are stored as
bytea and covered by the normal PostgreSQL backups.

Migration 155 and `repository/feedback.go` own the independent feedback tables.
Nothing creates operational Tasks or changes fleet/accounting/provider records.
Post and comment submission UUIDs make retries idempotent. A post digest binds
all normalized content and attachment bytes. Explicit vote PUTs are idempotent;
the database primary key prevents duplicate votes. Version checks reject stale
content/status edits rather than overwriting another user's change.

API routes (all session protected, mutations require CSRF):

- `GET/POST /feedback`
- `GET /feedback/tags`
- `GET /feedback/messages` and `/feedback/messages/count` (current user only)
- `PUT /feedback/messages/{id}/read` (current recipient only)
- `GET/PUT /feedback/{id}`
- `PUT /feedback/{id}/vote`
- `POST /feedback/{id}/comments`
- `GET /feedback/attachments/{id}` (`?download=1` forces download)

Validation:

```powershell
# Local disposable _test database with the mserp_app role
$env:MSERP_FEEDBACK_TEST_DATABASE_URL='postgres://.../mserp_feedback_test?sslmode=disable'
go test ./internal/httpapi -run TestFeedback -count=1
```

Run lint and a `/api` frontend build, start `frontend/scripts/review-local.mjs`
with a local disposable `MSERP_REVIEW_TEST_DATABASE_URL`, then run
`node scripts/test-feedback-e2e.mjs` with `MSERP_REVIEW_ACCESS_FILE` set to the
runner's private access JSON. This covers real sessions/API/storage, clipboard
images, upload previews, failure/retry, comments, author editing, independent
votes, administrator status/drag, mobile overflow and anonymous protection.
