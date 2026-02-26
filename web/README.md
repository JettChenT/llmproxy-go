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
This is a [Next.js](https://nextjs.org) project bootstrapped with [`create-next-app`](https://nextjs.org/docs/app/api-reference/cli/create-next-app).

## Getting Started

First, run the development server:

```bash
npm run dev
# or
yarn dev
# or
pnpm dev
# or
bun dev
```

Open [http://localhost:3000](http://localhost:3000) with your browser to see the result.

You can start editing the page by modifying `app/page.tsx`. The page auto-updates as you edit the file.

This project uses [`next/font`](https://nextjs.org/docs/app/building-your-application/optimizing/fonts) to automatically optimize and load [Geist](https://vercel.com/font), a new font family for Vercel.

## Learn More

To learn more about Next.js, take a look at the following resources:

- [Next.js Documentation](https://nextjs.org/docs) - learn about Next.js features and API.
- [Learn Next.js](https://nextjs.org/learn) - an interactive Next.js tutorial.

You can check out [the Next.js GitHub repository](https://github.com/vercel/next.js) - your feedback and contributions are welcome!

## Deploy on Vercel

The easiest way to deploy your Next.js app is to use the [Vercel Platform](https://vercel.com/new?utm_medium=default-template&filter=next.js&utm_source=create-next-app&utm_campaign=create-next-app-readme) from the creators of Next.js.

Check out our [Next.js deployment documentation](https://nextjs.org/docs/app/building-your-application/deploying) for more details.
