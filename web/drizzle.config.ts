import { defineConfig } from "drizzle-kit";

const databaseURL = process.env.DATABASE_URL ?? "file:./data/llmproxy-share.db";

export default defineConfig({
  dialect: "sqlite",
  schema: "./src/db/schema.ts",
  out: "./drizzle",
  dbCredentials: {
    url: databaseURL,
  },
  strict: true,
  verbose: true,
});
