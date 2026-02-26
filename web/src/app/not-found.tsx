export default function NotFound() {
  return (
    <main className="shell">
      <section className="hero">
        <p className="eyebrow">404</p>
        <h1>Share not found</h1>
        <p className="muted">
          This UUID does not exist, or the payload was removed from object storage.
        </p>
      </section>
    </main>
  );
}
