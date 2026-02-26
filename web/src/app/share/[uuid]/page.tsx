import { detectCodeLanguage, highlightToHTML } from "@/lib/syntax-highlight";
import { getShareByUUID } from "@/lib/shares";
import type { SharedRequest } from "@/lib/share-types";
import {
  buildRequestVisualization,
  formatHeaders,
  type VisualBlock,
  type VisualMessage,
  type VisualToolDefinition,
} from "@/lib/visualization";
import { notFound } from "next/navigation";

type Params = {
  uuid: string;
};

const ROLE_STYLE: Record<
  VisualMessage["role"],
  { label: string; roleClass: string }
> = {
  system: { label: "SYSTEM", roleClass: "role-system" },
  user: { label: "USER", roleClass: "role-user" },
  assistant: { label: "ASSISTANT", roleClass: "role-assistant" },
  tool: { label: "TOOL", roleClass: "role-tool" },
  unknown: { label: "UNKNOWN", roleClass: "role-unknown" },
};

function formatTimestamp(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function formatDuration(milliseconds: number) {
  if (milliseconds <= 0) {
    return "-";
  }
  if (milliseconds < 1000) {
    return `${milliseconds} ms`;
  }
  if (milliseconds < 10_000) {
    return `${(milliseconds / 1000).toFixed(2)} s`;
  }
  return `${(milliseconds / 1000).toFixed(1)} s`;
}

function formatCurrency(amount: number) {
  if (amount <= 0) {
    return "-";
  }
  if (amount < 0.0001) {
    return "<$0.0001";
  }
  if (amount < 0.01) {
    return `$${amount.toFixed(6)}`;
  }
  return `$${amount.toFixed(4)}`;
}

function formatBytes(value: number) {
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function formatTokenPair(request: SharedRequest) {
  if (request.input_tokens > 0 || request.output_tokens > 0) {
    return `${request.input_tokens}/${request.output_tokens}`;
  }
  if (request.estimated_input_tokens > 0) {
    return `~${request.estimated_input_tokens}/-`;
  }
  return "-";
}

function statusClass(statusText: string) {
  switch (statusText) {
    case "complete":
      return "badge badge-success";
    case "error":
      return "badge badge-error";
    default:
      return "badge badge-pending";
  }
}

function normalizeCode(raw: string, language: string) {
  if (!raw) {
    return "(empty)";
  }
  if (language !== "json") {
    return raw;
  }
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

async function HighlightedCode({
  raw,
  language,
  label,
}: {
  raw: string;
  language?: string;
  label?: string;
}) {
  const codeLanguage = language || detectCodeLanguage(raw);
  const normalized = normalizeCode(raw, codeLanguage);
  const html = await highlightToHTML(normalized, codeLanguage);

  return (
    <div className="highlightWrap">
      {label ? <p className="panelLabel">{label}</p> : null}
      <div
        className="codeBlockShiki"
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </div>
  );
}

function HeaderSection({
  title,
  headers,
}: {
  title: string;
  headers: Record<string, string[]> | undefined;
}) {
  const entries = formatHeaders(headers);
  return (
    <article className="panel compactPanel">
      <h3>{title}</h3>
      {entries.length === 0 ? (
        <p className="muted">(none)</p>
      ) : (
        <ul className="headerList">
          {entries.map((entry) => (
            <li key={`${title}-${entry.name}`}>
              <span className="headerKey">{entry.name}</span>
              <span className="headerValue">{entry.value}</span>
            </li>
          ))}
        </ul>
      )}
    </article>
  );
}

async function renderMessageBlock(block: VisualBlock, key: string) {
  if (block.type === "text") {
    return (
      <div key={key} className="messageBlock messageText">
        {block.text}
      </div>
    );
  }

  if (block.type === "thinking") {
    return (
      <div key={key} className="messageBlock messageThinking">
        <p className="panelLabel">Reasoning</p>
        <div className="messageText">{block.text}</div>
      </div>
    );
  }

  if (block.type === "note") {
    return (
      <div key={key} className="messageBlock messageNote">
        {block.text}
      </div>
    );
  }

  if (block.type === "image") {
    const tooLargeDataURL =
      block.isBase64 && block.src.startsWith("data:") && block.src.length > 700_000;
    if (tooLargeDataURL) {
      return (
        <div key={key} className="messageBlock messageWarning">
          Image omitted in browser due large embedded base64 payload.
        </div>
      );
    }

    return (
      <figure key={key} className="messageBlock imageFigure">
        <img src={block.src} alt="Request message content" className="messageImage" />
      </figure>
    );
  }

  return (
    <div key={key} className="messageBlock">
      <HighlightedCode
        raw={block.code}
        language={block.language}
        label={block.label || "Structured block"}
      />
    </div>
  );
}

async function ToolDefinitionList({
  definitions,
}: {
  definitions: VisualToolDefinition[];
}) {
  if (definitions.length === 0) {
    return null;
  }

  return (
    <section className="panel compactPanel">
      <h3>Tool definitions ({definitions.length})</h3>
      <div className="toolStack">
        {definitions.map((definition) => (
          <article key={definition.id} className="toolCard">
            <header>
              <span className="toolName">{definition.name}</span>
              {definition.description ? (
                <p className="muted">{definition.description}</p>
              ) : null}
            </header>
            <HighlightedCode
              raw={definition.schema}
              language="json"
              label="Schema"
            />
          </article>
        ))}
      </div>
    </section>
  );
}

async function MessageCard({ message }: { message: VisualMessage }) {
  const roleMeta = ROLE_STYLE[message.role] ?? ROLE_STYLE.unknown;
  const renderedBlocks = await Promise.all(
    message.blocks.map((block, index) =>
      renderMessageBlock(block, `${message.id}-${index}`),
    ),
  );

  return (
    <article className="messageCard">
      <header className="messageHeader">
        <div>
          <span className={`rolePill ${roleMeta.roleClass}`}>{roleMeta.label}</span>
          <span className="sourcePill">{message.source}</span>
        </div>
        <div className="messageMeta">
          <span>{message.title}</span>
          {message.subtitle ? <span className="muted">{message.subtitle}</span> : null}
        </div>
      </header>

      <div className="messageBody">{renderedBlocks}</div>

      {message.toolCalls.length > 0 ? (
        <section className="toolCallsSection">
          <p className="panelLabel">Tool calls ({message.toolCalls.length})</p>
          <div className="toolStack">
            {message.toolCalls.map((toolCall, index) => (
              <article
                key={`${message.id}-tool-${toolCall.id || index}`}
                className="toolCard"
              >
                <header>
                  <span className="toolName">{toolCall.name}</span>
                  {toolCall.id ? <span className="muted">{toolCall.id}</span> : null}
                </header>
                <HighlightedCode
                  raw={toolCall.arguments}
                  language="json"
                  label="Arguments"
                />
              </article>
            ))}
          </div>
        </section>
      ) : null}
    </article>
  );
}

async function RequestCard({
  request,
  defaultOpen,
}: {
  request: SharedRequest;
  defaultOpen: boolean;
}) {
  const visualization = buildRequestVisualization(request);
  const requestCode = request.request_body || "(empty)";
  const responseCode = request.response_body || "(empty)";

  return (
    <details className="requestCard requestDetails" open={defaultOpen}>
      <summary className="requestSummary">
        <div className="summaryMain">
          <h2>
            #{request.id} {request.method} {request.path}
          </h2>
          <p className="muted summarySubline">
            {request.model} · {request.provider_id || "unknown provider"} ·{" "}
            {formatTimestamp(request.start_time)}
          </p>
          {visualization.preview ? (
            <p className="summaryPreview">{visualization.preview}</p>
          ) : null}
        </div>
        <div className="summaryRight">
          <span className={statusClass(request.status_text)}>
            {request.status_text}
            {request.status_code > 0 ? ` · ${request.status_code}` : ""}
          </span>
          <div className="summaryChips">
            <span className="metricChip">
              <strong>Duration</strong> {formatDuration(request.duration_ms)}
            </span>
            <span className="metricChip">
              <strong>TTFT</strong> {formatDuration(request.ttft_ms)}
            </span>
            <span className="metricChip">
              <strong>Tokens</strong> {formatTokenPair(request)}
            </span>
            <span className="metricChip">
              <strong>Cost</strong> {formatCurrency(request.cost)}
            </span>
            <span className="metricChip">
              <strong>Size</strong>{" "}
              {formatBytes(request.request_size)} / {formatBytes(request.response_size)}
            </span>
            {request.cached_response ? (
              <span className="metricChip metricChipAccent">Cached</span>
            ) : null}
            {request.is_streaming ? (
              <span className="metricChip metricChipAccent">Streaming</span>
            ) : null}
          </div>
        </div>
      </summary>

      <div className="requestExpanded">
        <section className="panel">
          <div className="panelTopRow">
            <h3>Messages</h3>
            <span className="muted">{visualization.providerMode} format</span>
          </div>
          <div className="messageTimeline">
            {visualization.messages.length === 0 ? (
              <p className="muted">No messages parsed from payload.</p>
            ) : (
              await Promise.all(
                visualization.messages.map((message) => (
                  <MessageCard key={message.id} message={message} />
                )),
              )
            )}
          </div>
        </section>

        <ToolDefinitionList definitions={visualization.toolDefinitions} />

        <section className="observabilityGrid">
          <HeaderSection title="Request headers" headers={request.request_headers} />
          <HeaderSection title="Response headers" headers={request.response_headers} />
        </section>

        <section className="rawGrid">
          <article className="panel">
            <h3>Raw input</h3>
            <HighlightedCode raw={requestCode} language={detectCodeLanguage(requestCode)} />
            {request.request_body_truncated ? (
              <p className="warning">
                Request body was truncated by CLI session persistence.
              </p>
            ) : null}
          </article>
          <article className="panel">
            <h3>Raw output</h3>
            <HighlightedCode
              raw={responseCode}
              language={detectCodeLanguage(responseCode)}
            />
            {request.response_body_truncated ? (
              <p className="warning">
                Response body was truncated by CLI session persistence.
              </p>
            ) : null}
          </article>
        </section>
      </div>
    </details>
  );
}

export default async function SharePage({
  params,
}: {
  params: Promise<Params>;
}) {
  const { uuid } = await params;
  const share = await getShareByUUID(uuid);
  if (!share) {
    notFound();
  }

  const { snapshot } = share;
  const requestCount = snapshot.requests.length;
  const totalCost = snapshot.requests.reduce((total, req) => total + req.cost, 0);
  const totalInputTokens = snapshot.requests.reduce(
    (total, req) => total + req.input_tokens,
    0,
  );
  const totalOutputTokens = snapshot.requests.reduce(
    (total, req) => total + req.output_tokens,
    0,
  );

  return (
    <main className="shell">
      <section className="hero">
        <p className="eyebrow">llmproxy-go · shared debug trace</p>
        <h1>{snapshot.title || "Shared request trace"}</h1>
        <p className="muted">
          UUID: <code>{snapshot.share_uuid}</code>
        </p>
        <div className="metaGrid">
          <div>
            <span className="metricLabel">Session ID</span>
            <span className="metricValue">{snapshot.source.session_id}</span>
          </div>
          <div>
            <span className="metricLabel">Proxy</span>
            <span className="metricValue">{snapshot.source.listen_addr}</span>
          </div>
          <div>
            <span className="metricLabel">Target</span>
            <span className="metricValue">{snapshot.source.target_url}</span>
          </div>
          <div>
            <span className="metricLabel">Shared At</span>
            <span className="metricValue">{formatTimestamp(snapshot.created_at)}</span>
          </div>
          <div>
            <span className="metricLabel">Requests</span>
            <span className="metricValue">{requestCount}</span>
          </div>
          <div>
            <span className="metricLabel">Total Tokens</span>
            <span className="metricValue">
              {totalInputTokens}/{totalOutputTokens}
            </span>
          </div>
          <div>
            <span className="metricLabel">Total Cost</span>
            <span className="metricValue">{formatCurrency(totalCost)}</span>
          </div>
        </div>
      </section>

      <section className="contentStack">
        {await Promise.all(
          snapshot.requests.map((request, index) => (
            <RequestCard
              key={request.id}
              request={request}
              defaultOpen={index === 0}
            />
          )),
        )}
      </section>
    </main>
  );
}
