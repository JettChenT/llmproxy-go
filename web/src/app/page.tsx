export default function Home() {
  return (
    <main className="shell">
      <section className="hero">
        <p className="eyebrow">llmproxy-go · web share platform</p>
        <h1>Share LLM request traces with UUID links</h1>
        <p className="muted">
          Upload requests from the CLI and send teammates a stable link with rich
          request/response visualization.
        </p>
      </section>

      <section className="contentStack">
        <article className="panel">
          <h2>1) Start the web platform</h2>
          <pre className="codeBlock">bun run dev</pre>
        </article>

        <article className="panel">
          <h2>2) Share from the CLI</h2>
          <pre className="codeBlock">
            ./llmproxy-go share --session &lt;session-id&gt; --request &lt;request-id&gt;
          </pre>
          <p className="muted">
            The command uploads request payload(s) and prints a share URL.
          </p>
        </article>

        <article className="panel">
          <h2>3) Open the UUID link</h2>
          <p className="muted">
            Shared links are served at <code>/share/&lt;uuid&gt;</code>.
          </p>
        </article>
      </section>
    </main>
  );
}
