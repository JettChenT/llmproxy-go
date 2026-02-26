import { integer, sqliteTable, text, uniqueIndex } from "drizzle-orm/sqlite-core";

export const shares = sqliteTable(
  "shares",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    uuid: text("uuid").notNull(),
    title: text("title"),
    sourceSessionID: text("source_session_id").notNull(),
    sourceListenAddr: text("source_listen_addr").notNull(),
    sourceTargetURL: text("source_target_url").notNull(),
    requestCount: integer("request_count").notNull(),
    firstRequestID: integer("first_request_id"),
    objectKey: text("object_key").notNull(),
    createdAt: integer("created_at", { mode: "timestamp_ms" }).notNull(),
  },
  (table) => ({
    shareUUIDIdx: uniqueIndex("shares_uuid_idx").on(table.uuid),
  }),
);
