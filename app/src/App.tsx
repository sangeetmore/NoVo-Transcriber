import { useEffect, useMemo, useRef, useState } from "react";

import { useActivityStream } from "./hooks/useActivityStream";
import { getSessionStatus, startSession, stopSession } from "./services/backend";

type SessionStatus = "idle" | "starting" | "recording" | "stopping" | "error";
type LocalActivity = {
  type: "local_event";
  timestamp: number;
  category: string;
  icon?: string;
  label: string;
  detail?: string;
};

function formatElapsed(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(rest).padStart(2, "0")}`;
}

function formatTime(timestamp: number) {
  return new Date(timestamp * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function requireNoteItBridge() {
  if (!window.noteit?.startCapture || !window.noteit?.stopCapture || !window.noteit?.openExternal) {
    throw new Error("Electron bridge unavailable. Restart the Note It Electron app; do not use the browser tab.");
  }
  return window.noteit;
}

function activityKey(event: { timestamp: number; category: string; label: string; detail?: string }) {
  return `${event.timestamp}:${event.category}:${event.label}:${event.detail || ""}`;
}

export default function App() {
  const [sessionStatus, setSessionStatus] = useState<SessionStatus>("idle");
  const [notionPageUrl, setNotionPageUrl] = useState("");
  const [elapsedSeconds, setElapsedSeconds] = useState(0);
  const [errorMessage, setErrorMessage] = useState("");
  const [pipelineHint, setPipelineHint] = useState("");
  const [localEvents, setLocalEvents] = useState<LocalActivity[]>([]);
  const startedAtRef = useRef<number | null>(null);
  const activityListRef = useRef<HTMLDivElement | null>(null);
  const { events, connected, clearEvents } = useActivityStream();
  const visibleEvents = [...events, ...localEvents]
    .filter((event, index, all) => all.findIndex((candidate) => activityKey(candidate) === activityKey(event)) === index)
    .sort((a, b) => a.timestamp - b.timestamp)
    .slice(-100);

  const isBusy = sessionStatus === "starting" || sessionStatus === "stopping";
  const isRecording = sessionStatus === "recording";

  const statusLabel = useMemo(() => {
    if (sessionStatus === "starting") return "Starting";
    if (sessionStatus === "recording") return "Recording";
    if (sessionStatus === "stopping") return "Stopping";
    if (sessionStatus === "error") return "Needs attention";
    return "Ready";
  }, [sessionStatus]);

  useEffect(() => {
    if (!isRecording) return;
    const interval = window.setInterval(() => {
      if (!startedAtRef.current) return;
      setElapsedSeconds(Math.floor((Date.now() - startedAtRef.current) / 1000));
    }, 1000);
    return () => window.clearInterval(interval);
  }, [isRecording]);

  useEffect(() => {
    const list = activityListRef.current;
    if (!list) return;
    list.scrollTop = list.scrollHeight;
  }, [visibleEvents.length]);

  useEffect(() => {
    if (!window.noteit?.onCaptureLog) return;
    return window.noteit.onCaptureLog((payload) => {
      const isError = payload.level === "error";
      setLocalEvents((prev) => [
        ...prev.slice(-49),
        {
          type: "local_event",
          timestamp: payload.timestamp / 1000,
          category: isError ? "error" : "capture",
          icon: isError ? "⚠️" : "🎥",
          label: payload.message,
        },
      ]);
      if (payload.message.includes("Session started")) {
        setPipelineHint("Capture client started. Waiting for VideoDB RTStreams...");
      }
      if (payload.message.includes("Capture client exited") || isError) {
        setPipelineHint("Capture client reported an issue. Check the activity feed.");
      }
    });
  }, []);

  useEffect(() => {
    setLocalEvents((prev) => [
      ...prev,
      {
        type: "local_event",
        timestamp: Date.now() / 1000,
        category: window.noteit?.startCapture ? "capture" : "error",
        icon: window.noteit?.startCapture ? "🎥" : "⚠️",
        label: window.noteit?.startCapture
          ? "Electron bridge ready"
          : "Electron bridge unavailable. Capture cannot start from this window.",
      },
    ]);
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function syncStatus() {
      try {
        const status = await getSessionStatus();
        if (cancelled) return;

        if (status.status === "no_session") {
          if (sessionStatus !== "starting" && sessionStatus !== "stopping") {
            setNotionPageUrl("");
            setPipelineHint("");
            setSessionStatus("idle");
          }
          return;
        }

        if (status.notion_page_url) {
          setNotionPageUrl(status.notion_page_url);
        }

        if (status.status === "capturing") {
          if (!startedAtRef.current) {
            startedAtRef.current = Date.now();
          }
          if (sessionStatus !== "starting" && sessionStatus !== "stopping") {
            setSessionStatus("recording");
          }
          if (!status.display_rtstream_id && !status.audio_rtstream_id) {
            setPipelineHint("Waiting for screen/audio capture. Keep the capture permission dialog accepted.");
          } else {
            setPipelineHint("Capture streams connected. You can play your video now.");
          }
        }

        if (status.status === "stopped") {
          if (sessionStatus !== "starting" && sessionStatus !== "stopping") {
            setSessionStatus("idle");
            setPipelineHint("");
          }
        }

        if (status.status === "failed") {
          setSessionStatus("error");
          setErrorMessage(status.consumer_error || "Backend session failed");
        }
      } catch {
        if (!cancelled && sessionStatus === "recording") {
          setPipelineHint("Backend status temporarily unavailable.");
        }
      }
    }

    syncStatus();
    const interval = window.setInterval(syncStatus, 2500);
    return () => {
      cancelled = true;
      window.clearInterval(interval);
    };
  }, [sessionStatus]);

  async function handleStart() {
    setSessionStatus("starting");
    setErrorMessage("");
    clearEvents();
    setLocalEvents([]);
    setElapsedSeconds(0);

    try {
      const noteit = requireNoteItBridge();
      const data = await startSession();
      setNotionPageUrl(data.notion_page_url);
      const capture = await noteit.startCapture({
        capture_session_id: data.capture_session_id,
        client_token: data.client_token,
      });
      setLocalEvents((prev) => [
        ...prev.slice(-49),
        {
          type: "local_event",
          timestamp: Date.now() / 1000,
          category: "capture",
          icon: "🎥",
          label: `Capture process started${capture.pid ? ` (pid ${capture.pid})` : ""}`,
        },
      ]);
      startedAtRef.current = Date.now();
      setPipelineHint("Starting capture. Accept permissions, then play your video.");
      setSessionStatus("recording");
    } catch (error) {
      try {
        await stopSession();
      } catch {
        // Best effort cleanup if backend started but capture failed.
      }
      setSessionStatus("error");
      setErrorMessage(error instanceof Error ? error.message : "Failed to start session");
    }
  }

  async function handleStop() {
    setSessionStatus("stopping");
    setErrorMessage("");

    try {
      await requireNoteItBridge().stopCapture();
      await stopSession();
      startedAtRef.current = null;
      setElapsedSeconds(0);
      setNotionPageUrl("");
      setPipelineHint("");
      setSessionStatus("idle");
    } catch (error) {
      setSessionStatus("error");
      setErrorMessage(error instanceof Error ? error.message : "Failed to stop session");
    }
  }

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
          <span className="status-pill" data-state={sessionStatus}>
            <span className="dot" />
            {statusLabel}
          </span>
          <span className="timer">{formatElapsed(elapsedSeconds)}</span>
        </div>
        <button
          className={`primary-btn ${isRecording ? "stop-state" : ""}`}
          type="button"
          disabled={isBusy}
          onClick={isRecording ? handleStop : handleStart}
        >
          {sessionStatus === "starting" && "Starting..."}
          {sessionStatus === "stopping" && "Stopping..."}
          {sessionStatus === "recording" && "Stop Session"}
          {(sessionStatus === "idle" || sessionStatus === "error") && "Start Session"}
        </button>
        <p className={`hint ${isRecording ? "watch-now" : ""}`}>
          {isRecording
            ? pipelineHint || "Play your video now. Note It is watching."
            : "Start a session, then play your educational video."}
        </p>
        {errorMessage && <p className="error-text">{errorMessage}</p>}
      </section>

      <section className="notion-section">
        <div className="section-label">Notes</div>
        {notionPageUrl ? (
          <>
            <div className="notion-title">Note It session notes</div>
            <button
              className="link-btn"
              type="button"
              onClick={() => {
                try {
                  requireNoteItBridge().openExternal(notionPageUrl);
                } catch (error) {
                  setErrorMessage(error instanceof Error ? error.message : "Could not open Notion link");
                }
              }}
            >
              Open in browser
            </button>
          </>
        ) : (
          <div className="empty-state">Your Notion page link will appear here.</div>
        )}
      </section>

      <section className="activity-section">
        <div className="section-header">
          <span>Agent Activity</span>
          <span className={`connection-dot ${connected ? "connected" : ""}`} title={connected ? "Activity connected" : "Activity disconnected"} />
        </div>
        <div className="activity-list" ref={activityListRef}>
          {visibleEvents.length === 0 ? (
            <div className="activity-empty">Live backend events will stream here.</div>
          ) : (
            visibleEvents.map((event, index) => (
              <div className="activity-item" data-category={event.category} key={`${event.timestamp}-${index}`}>
                <span className="activity-dot" />
                <div className="activity-content">
                  <div className="activity-label">
                    {event.icon ? <span className="activity-icon">{event.icon}</span> : null}
                    {event.label}
                  </div>
                  {event.detail ? <div className="activity-detail">{event.detail}</div> : null}
                </div>
                <time className="activity-time">{formatTime(event.timestamp)}</time>
              </div>
            ))
          )}
        </div>
      </section>
    </main>
  );
}
