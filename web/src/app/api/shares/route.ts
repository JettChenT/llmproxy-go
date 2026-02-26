import { NextResponse } from "next/server";
import { createShare } from "@/lib/shares";
import { shareUploadRequestSchema } from "@/lib/share-types";

function getUploadToken(request: Request) {
  const explicitToken = request.headers.get("x-llmproxy-api-key");
  if (explicitToken) {
    return explicitToken.trim();
  }

  const authHeader = request.headers.get("authorization");
  if (!authHeader) {
    return "";
  }

  const [scheme, token] = authHeader.split(" ");
  if (scheme?.toLowerCase() !== "bearer" || !token) {
    return "";
  }
  return token.trim();
}

function isUploadAuthorized(request: Request) {
  const requiredToken = process.env.SHARE_UPLOAD_API_KEY?.trim();
  if (!requiredToken) {
    return true;
  }
  return getUploadToken(request) === requiredToken;
}

export async function POST(request: Request) {
  if (!isUploadAuthorized(request)) {
    return NextResponse.json(
      { error: "Unauthorized upload token." },
      { status: 401 },
    );
  }

  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "Invalid JSON body." }, { status: 400 });
  }

  const parsed = shareUploadRequestSchema.safeParse(body);
  if (!parsed.success) {
    return NextResponse.json(
      {
        error: "Invalid upload payload.",
        details: parsed.error.issues,
      },
      { status: 400 },
    );
  }

  try {
    const origin = new URL(request.url).origin;
    const created = await createShare(parsed.data, origin);
    return NextResponse.json(
      {
        share_uuid: created.shareUUID,
        share_url: created.shareURL,
        request_count: created.requestCount,
        created_at: created.createdAt,
      },
      { status: 201 },
    );
  } catch (error) {
    const message =
      error instanceof Error ? error.message : "Failed to create share.";
    return NextResponse.json({ error: message }, { status: 500 });
  }
}
