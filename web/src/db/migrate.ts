import path from "node:path";
import { migrate } from "drizzle-orm/libsql/migrator";
import { db } from "./client";

let migratePromise: Promise<void> | null = null;

export function ensureDatabase() {
  if (!migratePromise) {
    migratePromise = migrate(db, {
      migrationsFolder: path.join(process.cwd(), "drizzle"),
    });
  }
  return migratePromise;
}
