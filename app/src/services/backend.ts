const BACKEND_URL = import.meta.env.VITE_BACKEND_URL || "http://127.0.0.1:8000";
const WS_URL = BACKEND_URL.replace(/^http/, "ws") + "/ws/activity";

export type StartResponse = {
  session_id: string;
  capture_session_id: string;
  client_token: string;
  sandbox_id: string;
  notion_page_url: string;
};

export type SessionStatusResponse =
  | { status: "no_session" }
  | {
      session_id: string;
      status: "capturing" | "stopped" | "failed";
      capture_session_id: string;
      sandbox_id: string;
      display_rtstream_id: string;
      audio_rtstream_id: string;
      ws_connection_id: string;
      notion_page_url: string;
      windows_processed: number;
      windows_written: number;
      screenshots_captured: number;
      consumer_error?: string;
      consumer_task_done?: boolean;
    };

export type ActivityEvent = {
  type: string;
  timestamp: number;
  category: string;
  icon?: string;
  label: string;
  detail?: string;
  metadata?: Record<string, unknown>;
};

async function parseJsonResponse<T>(res: Response, action: string): Promise<T> {
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`${action} failed (${res.status})${body ? `: ${body}` : ""}`);
  }
  return res.json() as Promise<T>;
}

export async function startSession(): Promise<StartResponse> {
  const res = await fetch(`${BACKEND_URL}/api/session/start`, { method: "POST" });
  return parseJsonResponse<StartResponse>(res, "start session");
}

export async function stopSession(): Promise<unknown> {
  const res = await fetch(`${BACKEND_URL}/api/session/stop`, { method: "POST" });
  return parseJsonResponse<unknown>(res, "stop session");
}

export async function getSessionStatus(): Promise<SessionStatusResponse> {
  const res = await fetch(`${BACKEND_URL}/api/session/status`);
  return parseJsonResponse<SessionStatusResponse>(res, "get session status");
}

export function openActivitySocket(
  onEvent: (event: ActivityEvent) => void,
  onConnectedChange: (connected: boolean) => void,
): WebSocket {
  const ws = new WebSocket(WS_URL);

  ws.onopen = () => onConnectedChange(true);
  ws.onclose = () => onConnectedChange(false);
  ws.onerror = () => onConnectedChange(false);
  ws.onmessage = (message) => {
    try {
      const event = JSON.parse(message.data) as ActivityEvent;
      if (event.type === "pong") return;
      onEvent(event);
    } catch {
      // Ignore non-JSON keepalive noise.
    }
  };

  return ws;
}
