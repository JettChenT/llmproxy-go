import fs from "node:fs";
import path from "node:path";
import { createClient } from "@libsql/client";
import { drizzle } from "drizzle-orm/libsql";

const databaseURL = process.env.DATABASE_URL ?? "file:./data/llmproxy-share.db";

if (databaseURL.startsWith("file:")) {
  const filePath = databaseURL.replace(/^file:/, "");
  const resolvedPath = path.isAbsolute(filePath)
    ? filePath
    : path.join(process.cwd(), filePath);
  fs.mkdirSync(path.dirname(resolvedPath), { recursive: true });
}

const client = createClient({ url: databaseURL });

export const db = drizzle(client);
