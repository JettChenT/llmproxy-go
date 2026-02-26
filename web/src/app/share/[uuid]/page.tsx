import { getShareByUUID } from "@/lib/shares";
import type { SharedRequest } from "@/lib/share-types";
import { notFound } from "next/navigation";

type Params = {
  uuid: string;
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
  return `${(milliseconds / 1000).toFixed(2)} s`;
}

function formatCurrency(amount: number) {
  if (amount <= 0) {
    return "-";
  }
  return `$${amount.toFixed(6)}`;
}

function formatBody(raw: string) {
  if (!raw) {
    return "(empty)";
  }
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
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

function HeaderList({
  headers,
}: {
  headers: Record<string, string[]> | undefined;
}) {
  const entries = Object.entries(headers ?? {}).sort(([a], [b]) =>
    a.localeCompare(b),
  );

  if (entries.length === 0) {
    return <p className="muted">(none)</p>;
  }

  return (
    <ul className="headerList">
      {entries.map(([key, value]) => (
        <li key={key}>
          <span className="headerKey">{key}</span>
          <span className="headerValue">{value.join(", ")}</span>
        </li>
      ))}
    </ul>
  );
}

function RequestCard({ request }: { request: SharedRequest }) {
  return (
    <article className="requestCard">
      <header className="requestHeader">
        <h2>
          #{request.id} {request.method} {request.path}
        </h2>
        <span className={statusClass(request.status_text)}>
          {request.status_text}
          {request.status_code > 0 ? ` · ${request.status_code}` : ""}
        </span>
      </header>

      <p className="muted compact">
        Model: <strong>{request.model}</strong> · Provider:{" "}
        <strong>{request.provider_id || "unknown"}</strong> · Started:{" "}
        <strong>{formatTimestamp(request.start_time)}</strong>
      </p>

      <div className="metricsRow">
        <div>
          <span className="metricLabel">Duration</span>
          <span className="metricValue">{formatDuration(request.duration_ms)}</span>
        </div>
        <div>
          <span className="metricLabel">TTFT</span>
          <span className="metricValue">{formatDuration(request.ttft_ms)}</span>
        </div>
        <div>
          <span className="metricLabel">Tokens</span>
          <span className="metricValue">
            {request.input_tokens}/{request.output_tokens}
          </span>
        </div>
        <div>
          <span className="metricLabel">Cost</span>
          <span className="metricValue">{formatCurrency(request.cost)}</span>
        </div>
      </div>

      <div className="requestGrid">
        <section className="panel">
          <h3>Request</h3>
          <p className="panelLabel">Headers</p>
          <HeaderList headers={request.request_headers} />
          <p className="panelLabel">Body</p>
          <pre className="codeBlock">{formatBody(request.request_body ?? "")}</pre>
          {request.request_body_truncated ? (
            <p className="warning">
              Request body was truncated by the CLI session history.
            </p>
          ) : null}
        </section>

        <section className="panel">
          <h3>Response</h3>
          <p className="panelLabel">Headers</p>
          <HeaderList headers={request.response_headers} />
          <p className="panelLabel">Body</p>
          <pre className="codeBlock">{formatBody(request.response_body ?? "")}</pre>
          {request.response_body_truncated ? (
            <p className="warning">
              Response body was truncated by the CLI session history.
            </p>
          ) : null}
        </section>
      </div>
    </article>
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
        </div>
      </section>

      <section className="contentStack">
        {snapshot.requests.map((request) => (
          <RequestCard key={request.id} request={request} />
        ))}
      </section>
    </main>
  );
}
