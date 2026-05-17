export default function App() {
  return (
    <main className="app-frame">
      <div className="title-bar">
        <div className="drag-handle" aria-label="Drag Note It window" />
      </div>

      <section className="hero">
        <h1 className="brand">
          note<span className="brand-dot">·</span>it
        </h1>
        <p className="tagline">Watch any video. Get clean notes.</p>
      </section>

      <section className="session-card">
        <div className="status-row">
          <span className="status-pill" data-state="idle">
            <span className="dot" />
            Ready
          </span>
          <span className="timer">00:00</span>
        </div>
        <button className="primary-btn" type="button">
          Start Session
        </button>
        <p className="hint">Start a session, then play your educational video.</p>
      </section>

      <section className="notion-section">
        <div className="section-label">Notes</div>
        <div className="empty-state">Your Notion page link will appear here.</div>
      </section>

      <section className="activity-section">
        <div className="section-header">
          <span>Agent Activity</span>
        </div>
        <div className="activity-empty">Live backend events will stream here.</div>
      </section>
    </main>
  );
}
