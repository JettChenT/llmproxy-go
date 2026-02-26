import { z } from "zod";

const headersSchema = z.record(z.string(), z.array(z.string()));

export const sharedRequestSchema = z.object({
  id: z.number().int().nonnegative(),
  method: z.string().min(1),
  path: z.string().min(1),
  url: z.string().min(1),
  model: z.string().min(1),
  status: z.number().int(),
  status_text: z.string().min(1),
  status_code: z.number().int().nonnegative(),
  start_time: z.string().min(1),
  duration_ms: z.number().int().nonnegative(),
  ttft_ms: z.number().int().nonnegative(),
  request_headers: headersSchema.optional().default({}),
  response_headers: headersSchema.optional().default({}),
  request_body: z.string().optional().default(""),
  response_body: z.string().optional().default(""),
  request_body_truncated: z.boolean().optional().default(false),
  response_body_truncated: z.boolean().optional().default(false),
  request_size: z.number().int().nonnegative(),
  response_size: z.number().int().nonnegative(),
  is_streaming: z.boolean(),
  cached_response: z.boolean(),
  proxy_name: z.string().optional().default(""),
  proxy_listen: z.string().optional().default(""),
  estimated_input_tokens: z.number().int().nonnegative(),
  input_tokens: z.number().int().nonnegative(),
  output_tokens: z.number().int().nonnegative(),
  provider_id: z.string().optional().default(""),
  cost: z.number().nonnegative(),
});

export const shareSourceSchema = z.object({
  session_id: z.string().min(1),
  listen_addr: z.string().min(1),
  target_url: z.string().min(1),
});

export const shareUploadRequestSchema = z.object({
  title: z.string().trim().min(1).max(120).optional(),
  source: shareSourceSchema,
  requests: z.array(sharedRequestSchema).min(1).max(200),
});

export const shareSnapshotSchema = shareUploadRequestSchema.extend({
  share_uuid: z.string().uuid(),
  created_at: z.string().min(1),
});

export type SharedRequest = z.infer<typeof sharedRequestSchema>;
export type ShareUploadRequest = z.infer<typeof shareUploadRequestSchema>;
export type ShareSnapshot = z.infer<typeof shareSnapshotSchema>;
