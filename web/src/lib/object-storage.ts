import { GetObjectCommand, PutObjectCommand, S3Client } from "@aws-sdk/client-s3";
import fs from "node:fs/promises";
import path from "node:path";

interface ObjectStorage {
  putJSON(key: string, payload: unknown): Promise<void>;
  getJSON<T>(key: string): Promise<T>;
}

class LocalObjectStorage implements ObjectStorage {
  constructor(private readonly rootDir: string) {}

  async putJSON(key: string, payload: unknown) {
    const filePath = path.join(this.rootDir, key);
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await fs.writeFile(filePath, JSON.stringify(payload, null, 2), "utf8");
  }

  async getJSON<T>(key: string) {
    const filePath = path.join(this.rootDir, key);
    const raw = await fs.readFile(filePath, "utf8");
    return JSON.parse(raw) as T;
  }
}

class S3ObjectStorage implements ObjectStorage {
  constructor(
    private readonly client: S3Client,
    private readonly bucket: string,
  ) {}

  async putJSON(key: string, payload: unknown) {
    await this.client.send(
      new PutObjectCommand({
        Bucket: this.bucket,
        Key: key,
        Body: JSON.stringify(payload),
        ContentType: "application/json",
      }),
    );
  }

  async getJSON<T>(key: string) {
    const output = await this.client.send(
      new GetObjectCommand({
        Bucket: this.bucket,
        Key: key,
      }),
    );

    if (!output.Body) {
      throw new Error(`Object ${key} has an empty body.`);
    }

    const body = await streamBodyToString(output.Body);
    return JSON.parse(body) as T;
  }
}

async function streamBodyToString(body: unknown) {
  if (
    body &&
    typeof body === "object" &&
    "transformToString" in body &&
    typeof body.transformToString === "function"
  ) {
    return body.transformToString();
  }

  const chunks: Uint8Array[] = [];
  for await (const chunk of body as AsyncIterable<Uint8Array>) {
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString("utf8");
}

function createObjectStorage(): ObjectStorage {
  const bucket = process.env.OBJECT_STORAGE_BUCKET;
  if (!bucket) {
    const localDir =
      process.env.OBJECT_STORAGE_LOCAL_DIR ?? path.join(process.cwd(), ".storage");
    return new LocalObjectStorage(localDir);
  }

  const endpoint = process.env.OBJECT_STORAGE_ENDPOINT;
  const accessKeyID = process.env.OBJECT_STORAGE_ACCESS_KEY_ID;
  const secretAccessKey = process.env.OBJECT_STORAGE_SECRET_ACCESS_KEY;

  const client = new S3Client({
    region: process.env.OBJECT_STORAGE_REGION ?? "us-east-1",
    endpoint,
    forcePathStyle: process.env.OBJECT_STORAGE_FORCE_PATH_STYLE !== "false",
    credentials:
      accessKeyID && secretAccessKey
        ? {
            accessKeyId: accessKeyID,
            secretAccessKey,
          }
        : undefined,
  });

  return new S3ObjectStorage(client, bucket);
}

export const objectStorage = createObjectStorage();
