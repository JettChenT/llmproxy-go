# llmproxy-go share web platform

Next.js + Bun web app used by `llmproxy-go share` to store and visualize request traces.

## Stack

- Next.js App Router
- Bun runtime/package manager
- Drizzle ORM + SQLite (`@libsql/client`)
- S3-compatible object storage (`@aws-sdk/client-s3`) with local filesystem fallback

## Run locally

```bash
bun install
bun run db:generate
bun run db:migrate
bun run dev
```

The app runs at `http://localhost:3000`.

## Environment variables

Copy `.env.example` to `.env.local` and adjust as needed.

- `DATABASE_URL` (default: `file:./data/llmproxy-share.db`)
- `SHARE_UPLOAD_API_KEY` optional API key required by `POST /api/shares`
- `OBJECT_STORAGE_BUCKET` if set, enables S3-compatible storage
- `OBJECT_STORAGE_ENDPOINT` optional S3 endpoint (for MinIO/R2/etc)
- `OBJECT_STORAGE_REGION` default `us-east-1`
- `OBJECT_STORAGE_ACCESS_KEY_ID` + `OBJECT_STORAGE_SECRET_ACCESS_KEY` optional credentials
- `OBJECT_STORAGE_FORCE_PATH_STYLE` default `true`
- `OBJECT_STORAGE_LOCAL_DIR` local fallback directory when no bucket is configured

## API

- `POST /api/shares`: upload CLI payload and receive UUID link
- `GET /api/shares/:uuid`: fetch stored payload by UUID
- `GET /share/:uuid`: human-friendly request/response visualization page
