import type { SharedRequest } from "@/lib/share-types";

export type VisualBlock =
  | {
      type: "text";
      text: string;
    }
  | {
      type: "thinking";
      text: string;
    }
  | {
      type: "note";
      text: string;
    }
  | {
      type: "image";
      src: string;
      isBase64: boolean;
    }
  | {
      type: "code";
      code: string;
      language: string;
      label?: string;
    };

export type VisualToolCall = {
  id: string;
  name: string;
  arguments: string;
};

export type VisualMessage = {
  id: string;
  role: "system" | "user" | "assistant" | "tool" | "unknown";
  source: "request" | "response";
  title: string;
  subtitle?: string;
  blocks: VisualBlock[];
  toolCalls: VisualToolCall[];
};

export type VisualToolDefinition = {
  id: string;
  name: string;
  description: string;
  schema: string;
};

export type RequestVisualization = {
  providerMode: "openai" | "anthropic" | "unknown";
  preview: string;
  messages: VisualMessage[];
  toolDefinitions: VisualToolDefinition[];
};

function parseJSON(value: string) {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function detectProviderMode(
  request: SharedRequest,
  requestJSON: unknown,
): RequestVisualization["providerMode"] {
  if (
    request.path.endsWith("/v1/messages") ||
    request.provider_id.toLowerCase().includes("anthropic")
  ) {
    return "anthropic";
  }
  if (isRecord(requestJSON) && Array.isArray(requestJSON.messages)) {
    return "openai";
  }
  return "unknown";
}

function normalizeWhitespace(value: string) {
  return value.replace(/\s+/g, " ").trim();
}

function messagePreview(messages: VisualMessage[]) {
  for (const message of messages) {
    const textBlock = message.blocks.find(
      (block): block is Extract<VisualBlock, { type: "text" | "thinking" }> =>
        block.type === "text" || block.type === "thinking",
    );
    if (textBlock && normalizeWhitespace(textBlock.text)) {
      return normalizeWhitespace(textBlock.text).slice(0, 180);
    }
  }
  return "";
}

function maskHeaderValue(headerName: string, headerValue: string) {
  if (headerName.toLowerCase() !== "authorization") {
    return headerValue;
  }
  if (headerValue.length <= 18) {
    return "***";
  }
  return `${headerValue.slice(0, 8)}...${headerValue.slice(-6)}`;
}

export function formatHeaders(headers: Record<string, string[]> | undefined) {
  const entries = Object.entries(headers ?? {}).sort(([a], [b]) =>
    a.localeCompare(b),
  );
  return entries.map(([name, values]) => ({
    name,
    value: maskHeaderValue(name, values.join(", ")),
  }));
}

function stringifyJSON(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function extractOpenAIContentBlocks(content: unknown) {
  const blocks: VisualBlock[] = [];

  if (typeof content === "string") {
    return [{ type: "text", text: content }];
  }

  if (Array.isArray(content)) {
    for (const part of content) {
      if (!isRecord(part)) {
        continue;
      }
      const type = typeof part.type === "string" ? part.type : "";
      if (
        type === "text" ||
        type === "input_text" ||
        type === "output_text"
      ) {
        if (typeof part.text === "string" && part.text) {
          blocks.push({ type: "text", text: part.text });
        }
        continue;
      }

      if (type === "image_url") {
        let url = "";
        if (isRecord(part.image_url) && typeof part.image_url.url === "string") {
          url = part.image_url.url;
        } else if (typeof part.image_url === "string") {
          url = part.image_url;
        }
        if (url) {
          blocks.push({
            type: "image",
            src: url,
            isBase64: url.startsWith("data:"),
          });
        }
        continue;
      }

      if (typeof part.text === "string" && part.text) {
        blocks.push({ type: "text", text: part.text });
      } else {
        blocks.push({
          type: "code",
          code: stringifyJSON(part),
          language: "json",
          label: type ? `Block: ${type}` : "Block",
        });
      }
    }
    return blocks;
  }

  if (isRecord(content) && typeof content.text === "string") {
    return [{ type: "text", text: content.text }];
  }

  if (content !== null && content !== undefined) {
    blocks.push({
      type: "code",
      code: stringifyJSON(content),
      language: "json",
      label: "Content",
    });
  }

  return blocks;
}

function parseOpenAIToolCalls(raw: unknown) {
  const toolCalls: VisualToolCall[] = [];
  if (!Array.isArray(raw)) {
    return toolCalls;
  }

  for (const entry of raw) {
    if (!isRecord(entry)) {
      continue;
    }
    const functionData = isRecord(entry.function) ? entry.function : null;
    const id = typeof entry.id === "string" ? entry.id : "";
    const name =
      functionData && typeof functionData.name === "string"
        ? functionData.name
        : "unknown";
    const argumentsText =
      functionData && typeof functionData.arguments === "string"
        ? functionData.arguments
        : "{}";
    toolCalls.push({ id, name, arguments: argumentsText });
  }
  return toolCalls;
}

function parseAnthropicImageSource(block: Record<string, unknown>) {
  const source = isRecord(block.source) ? block.source : null;
  if (!source) {
    return "";
  }
  const sourceType = typeof source.type === "string" ? source.type : "";
  if (sourceType === "url" && typeof source.url === "string") {
    return source.url;
  }
  if (sourceType === "base64" && typeof source.data === "string") {
    const mediaType =
      typeof source.media_type === "string" ? source.media_type : "image/png";
    return `data:${mediaType};base64,${source.data}`;
  }
  return "";
}

function extractAnthropicContentBlocks(content: unknown) {
  const blocks: VisualBlock[] = [];
  const toolCalls: VisualToolCall[] = [];

  if (typeof content === "string") {
    blocks.push({ type: "text", text: content });
    return { blocks, toolCalls, toolResultID: "" };
  }

  if (!Array.isArray(content)) {
    if (content !== null && content !== undefined) {
      blocks.push({
        type: "code",
        code: stringifyJSON(content),
        language: "json",
      });
    }
    return { blocks, toolCalls, toolResultID: "" };
  }

  let toolResultID = "";
  for (const entry of content) {
    if (!isRecord(entry)) {
      continue;
    }
    const type = typeof entry.type === "string" ? entry.type : "";

    if (type === "text") {
      if (typeof entry.text === "string" && entry.text) {
        blocks.push({ type: "text", text: entry.text });
      }
      continue;
    }

    if (type === "thinking") {
      if (typeof entry.thinking === "string" && entry.thinking) {
        blocks.push({ type: "thinking", text: entry.thinking });
      }
      continue;
    }

    if (type === "redacted_thinking") {
      blocks.push({
        type: "note",
        text: "Thinking block is redacted by provider.",
      });
      continue;
    }

    if (type === "image") {
      const src = parseAnthropicImageSource(entry);
      if (src) {
        blocks.push({
          type: "image",
          src,
          isBase64: src.startsWith("data:"),
        });
      }
      continue;
    }

    if (type === "tool_use") {
      const name = typeof entry.name === "string" ? entry.name : "unknown";
      const id = typeof entry.id === "string" ? entry.id : "";
      toolCalls.push({
        id,
        name,
        arguments: stringifyJSON(entry.input ?? {}),
      });
      continue;
    }

    if (type === "tool_result") {
      toolResultID =
        typeof entry.tool_use_id === "string" ? entry.tool_use_id : toolResultID;
      const toolContent = entry.content;
      if (typeof toolContent === "string") {
        blocks.push({ type: "text", text: toolContent });
      } else if (Array.isArray(toolContent)) {
        const nested = extractAnthropicContentBlocks(toolContent);
        blocks.push(...nested.blocks);
      } else if (toolContent !== null && toolContent !== undefined) {
        blocks.push({
          type: "code",
          code: stringifyJSON(toolContent),
          language: "json",
          label: "tool_result",
        });
      }
      continue;
    }

    blocks.push({
      type: "code",
      code: stringifyJSON(entry),
      language: "json",
      label: type || "block",
    });
  }

  return { blocks, toolCalls, toolResultID };
}

function readToolDefinitions(requestJSON: unknown) {
  if (!isRecord(requestJSON) || !Array.isArray(requestJSON.tools)) {
    return [] as VisualToolDefinition[];
  }

  const definitions: VisualToolDefinition[] = [];
  for (const [index, tool] of requestJSON.tools.entries()) {
    if (!isRecord(tool)) {
      continue;
    }

    const functionEntry = isRecord(tool.function) ? tool.function : null;
    const openAIName =
      functionEntry && typeof functionEntry.name === "string"
        ? functionEntry.name
        : "";
    const anthropicName = typeof tool.name === "string" ? tool.name : "";
    const name = openAIName || anthropicName || `tool-${index + 1}`;

    const openAIDescription =
      functionEntry && typeof functionEntry.description === "string"
        ? functionEntry.description
        : "";
    const anthropicDescription =
      typeof tool.description === "string" ? tool.description : "";
    const description = openAIDescription || anthropicDescription;

    const schema =
      (functionEntry?.parameters as unknown) ?? tool.input_schema ?? {};

    definitions.push({
      id: `${name}-${index}`,
      name,
      description,
      schema: stringifyJSON(schema),
    });
  }
  return definitions;
}

function pushFallbackRawMessage(
  messages: VisualMessage[],
  source: "request" | "response",
  rawBody: string,
) {
  if (!rawBody) {
    return;
  }
  messages.push({
    id: `fallback-${source}`,
    role: source === "request" ? "user" : "assistant",
    source,
    title: source === "request" ? "Request payload" : "Response payload",
    blocks: [
      {
        type: "code",
        code: rawBody,
        language: "json",
      },
    ],
    toolCalls: [],
  });
}

function parseOpenAIMessages(request: SharedRequest, requestJSON: unknown, responseJSON: unknown) {
  const messages: VisualMessage[] = [];

  if (isRecord(requestJSON) && Array.isArray(requestJSON.messages)) {
    for (const [index, entry] of requestJSON.messages.entries()) {
      if (!isRecord(entry)) {
        continue;
      }
      const role =
        typeof entry.role === "string" ? (entry.role as VisualMessage["role"]) : "unknown";
      const blocks = extractOpenAIContentBlocks(entry.content);
      if (typeof entry.reasoning_content === "string" && entry.reasoning_content) {
        blocks.unshift({ type: "thinking", text: entry.reasoning_content });
      }
      if (typeof entry.tool_call_id === "string" && entry.tool_call_id) {
        blocks.unshift({
          type: "note",
          text: `Tool response for call: ${entry.tool_call_id}`,
        });
      }
      messages.push({
        id: `request-${index}`,
        role,
        source: "request",
        title: `${role || "unknown"} message`,
        blocks,
        toolCalls: parseOpenAIToolCalls(entry.tool_calls),
      });
    }
  } else {
    pushFallbackRawMessage(messages, "request", request.request_body);
  }

  if (isRecord(responseJSON) && Array.isArray(responseJSON.choices)) {
    for (const [index, choice] of responseJSON.choices.entries()) {
      if (!isRecord(choice)) {
        continue;
      }
      const message = isRecord(choice.message) ? choice.message : {};
      const finishReason =
        typeof choice.finish_reason === "string" ? choice.finish_reason : "";
      const blocks = extractOpenAIContentBlocks(message.content);
      if (finishReason) {
        blocks.push({ type: "note", text: `Finish reason: ${finishReason}` });
      }
      if (
        typeof message.reasoning_content === "string" &&
        message.reasoning_content
      ) {
        blocks.unshift({ type: "thinking", text: message.reasoning_content });
      }

      messages.push({
        id: `response-choice-${index}`,
        role: "assistant",
        source: "response",
        title: "assistant response",
        subtitle: `choice ${index}`,
        blocks,
        toolCalls: parseOpenAIToolCalls(message.tool_calls),
      });
    }
  } else if (request.response_body) {
    pushFallbackRawMessage(messages, "response", request.response_body);
  }

  return messages;
}

function parseAnthropicMessages(
  request: SharedRequest,
  requestJSON: unknown,
  responseJSON: unknown,
) {
  const messages: VisualMessage[] = [];

  if (isRecord(requestJSON)) {
    if (requestJSON.system !== undefined && requestJSON.system !== null) {
      const systemContent = extractAnthropicContentBlocks(requestJSON.system);
      messages.push({
        id: "system-0",
        role: "system",
        source: "request",
        title: "system prompt",
        blocks: systemContent.blocks,
        toolCalls: [],
      });
    }

    if (Array.isArray(requestJSON.messages)) {
      for (const [index, entry] of requestJSON.messages.entries()) {
        if (!isRecord(entry)) {
          continue;
        }
        const role =
          typeof entry.role === "string"
            ? (entry.role as VisualMessage["role"])
            : "unknown";
        const parsedContent = extractAnthropicContentBlocks(entry.content);
        if (parsedContent.toolResultID) {
          parsedContent.blocks.unshift({
            type: "note",
            text: `Tool result for call: ${parsedContent.toolResultID}`,
          });
        }

        messages.push({
          id: `request-anthropic-${index}`,
          role,
          source: "request",
          title: `${role || "unknown"} message`,
          blocks: parsedContent.blocks,
          toolCalls: parsedContent.toolCalls,
        });
      }
    }
  } else {
    pushFallbackRawMessage(messages, "request", request.request_body);
  }

  if (isRecord(responseJSON) && Array.isArray(responseJSON.content)) {
    const parsed = extractAnthropicContentBlocks(responseJSON.content);
    const stopReason =
      typeof responseJSON.stop_reason === "string" ? responseJSON.stop_reason : "";
    if (stopReason) {
      parsed.blocks.push({ type: "note", text: `Stop reason: ${stopReason}` });
    }
    messages.push({
      id: "response-anthropic-0",
      role: "assistant",
      source: "response",
      title: "assistant response",
      blocks: parsed.blocks,
      toolCalls: parsed.toolCalls,
    });
  } else if (request.response_body) {
    pushFallbackRawMessage(messages, "response", request.response_body);
  }

  return messages;
}

export function buildRequestVisualization(
  request: SharedRequest,
): RequestVisualization {
  const requestJSON = parseJSON(request.request_body ?? "");
  const responseJSON = parseJSON(request.response_body ?? "");
  const providerMode = detectProviderMode(request, requestJSON);
  const toolDefinitions = readToolDefinitions(requestJSON);

  let messages: VisualMessage[];
  if (providerMode === "anthropic") {
    messages = parseAnthropicMessages(request, requestJSON, responseJSON);
  } else if (providerMode === "openai") {
    messages = parseOpenAIMessages(request, requestJSON, responseJSON);
  } else {
    messages = [];
    pushFallbackRawMessage(messages, "request", request.request_body);
    pushFallbackRawMessage(messages, "response", request.response_body);
  }

  return {
    providerMode,
    preview: messagePreview(messages),
    messages,
    toolDefinitions,
  };
}
