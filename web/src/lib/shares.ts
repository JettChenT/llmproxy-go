import { eq } from "drizzle-orm";
import { db } from "@/db/client";
import { ensureDatabase } from "@/db/migrate";
import { shares } from "@/db/schema";
import { objectStorage } from "@/lib/object-storage";
import { ShareSnapshot, ShareUploadRequest, shareSnapshotSchema } from "@/lib/share-types";

type ShareRow = typeof shares.$inferSelect;

export async function createShare(
  upload: ShareUploadRequest,
  platformOrigin: string,
) {
  await ensureDatabase();

  const shareUUID = crypto.randomUUID();
  const createdAt = new Date();
  const objectKey = `shares/${shareUUID}.json`;

  const snapshot: ShareSnapshot = {
    share_uuid: shareUUID,
    created_at: createdAt.toISOString(),
    ...upload,
  };

  await objectStorage.putJSON(objectKey, snapshot);
  await db.insert(shares).values({
    uuid: shareUUID,
    title: upload.title ?? null,
    sourceSessionID: upload.source.session_id,
    sourceListenAddr: upload.source.listen_addr,
    sourceTargetURL: upload.source.target_url,
    requestCount: upload.requests.length,
    firstRequestID: upload.requests[0]?.id ?? null,
    objectKey,
    createdAt,
  });

  return {
    shareUUID,
    shareURL: new URL(`/share/${shareUUID}`, platformOrigin).toString(),
    requestCount: upload.requests.length,
    createdAt: snapshot.created_at,
  };
}

export async function getShareByUUID(uuid: string) {
  await ensureDatabase();

  const rows = await db
    .select()
    .from(shares)
    .where(eq(shares.uuid, uuid))
    .limit(1);

  const row = rows[0];
  if (!row) {
    return null;
  }

  const payload = await objectStorage.getJSON<unknown>(row.objectKey);
  const snapshot = shareSnapshotSchema.parse(payload);
  return {
    row,
    snapshot,
  };
}

export type StoredShare = {
  row: ShareRow;
  snapshot: ShareSnapshot;
};
