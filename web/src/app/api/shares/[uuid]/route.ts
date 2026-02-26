import { NextResponse } from "next/server";
import { getShareByUUID } from "@/lib/shares";

type Params = {
  uuid: string;
};

export async function GET(
  _request: Request,
  context: { params: Promise<Params> },
) {
  const { uuid } = await context.params;
  if (!uuid) {
    return NextResponse.json({ error: "Missing share UUID." }, { status: 400 });
  }

  try {
    const share = await getShareByUUID(uuid);
    if (!share) {
      return NextResponse.json({ error: "Share not found." }, { status: 404 });
    }

    return NextResponse.json(
      {
        share_uuid: share.snapshot.share_uuid,
        created_at: share.snapshot.created_at,
        title: share.snapshot.title ?? null,
        source: share.snapshot.source,
        requests: share.snapshot.requests,
      },
      { status: 200 },
    );
  } catch (error) {
    const message =
      error instanceof Error ? error.message : "Failed to load share.";
    return NextResponse.json({ error: message }, { status: 500 });
  }
}
