// Upload helpers (server-architecture.md 4.13, data-model.md 5). Plan 01 owns r2Key/removeUploads only;
// plan 02 adds sniffImage and the /v1/uploads routes.
import type { Env } from "../env";
import { all, jsonList, run } from "./db";
import { now } from "./time";

type UploadPurpose = "avatar" | "post" | "story";

const PURPOSE_DIR: Record<UploadPurpose, string> = { avatar: "avatars", post: "posts", story: "stories" };

/** R2 object key: "<avatars|posts|stories>/<owner_user_id>/<upload_id>.<ext>". */
export function r2Key(purpose: UploadPurpose, ownerId: string, uploadId: string, ext: string): string {
  return `${PURPOSE_DIR[purpose]}/${ownerId}/${uploadId}.${ext}`;
}

/** R2 accepts up to 1000 keys per delete call. */
const R2_DELETE_CHUNK = 1000;

/**
 * Deletes the R2 objects of the given uploads, then marks them status='removed', removed_at=now.
 * Unknown or already removed ids are skipped. R2 is deleted first so a failure leaves the row retryable.
 */
export async function removeUploads(env: Env, uploadIds: string[]): Promise<void> {
  const ids = [...new Set(uploadIds)];
  if (ids.length === 0) return;
  const rows = await all<{ id: string; r2_key: string }>(
    env.DB,
    "SELECT id, r2_key FROM uploads WHERE id IN (SELECT value FROM json_each(?1)) AND status <> 'removed'",
    jsonList(ids),
  );
  if (rows.length === 0) return;
  const keys = rows.map((r) => r.r2_key);
  for (let i = 0; i < keys.length; i += R2_DELETE_CHUNK) await env.MEDIA.delete(keys.slice(i, i + R2_DELETE_CHUNK));
  await run(
    env.DB,
    "UPDATE uploads SET status = 'removed', removed_at = ?2 WHERE id IN (SELECT value FROM json_each(?1)) AND status <> 'removed'",
    jsonList(rows.map((r) => r.id)),
    now(),
  );
}
