const BACKEND_URL = import.meta.env.VITE_BACKEND_URL || "http://127.0.0.1:8000";
const WS_URL = BACKEND_URL.replace(/^http/, "ws") + "/ws/activity";
const AUDIO_WS_URL = BACKEND_URL.replace(/^http/, "ws") + "/ws/audio";

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

export async function startAudioCapture(
  onLog: (msg: string, isError?: boolean) => void,
  onVolume?: (level: number) => void
): Promise<{ stop: () => void }> {
  try {
    let stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: false,
        noiseSuppression: false,
        autoGainControl: false,
      },
      video: false,
    });

    // Try to find a "Monitor" device (system audio on Linux)
    const devices = await navigator.mediaDevices.enumerateDevices();
    const monitorDevice = devices.find(
      (d) => d.kind === "audioinput" && d.label.toLowerCase().includes("monitor")
    );

    if (monitorDevice) {
      // Re-request stream with the monitor device
      stream.getTracks().forEach((t) => t.stop());
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          deviceId: { exact: monitorDevice.deviceId },
          echoCancellation: false,
          noiseSuppression: false,
          autoGainControl: false,
        },
        video: false,
      });
      onLog(`Using system audio device: ${monitorDevice.label}`);
    } else {
      onLog("Using default microphone (monitor device not found)");
    }

    const ws = new WebSocket(AUDIO_WS_URL);
    let recorder: MediaRecorder | null = null;
    let audioCtx: AudioContext | null = null;
    let analyser: AnalyserNode | null = null;
    let micSource: MediaStreamAudioSourceNode | null = null;
    let volumeInterval: number | null = null;

    if (onVolume) {
      audioCtx = new AudioContext();
      analyser = audioCtx.createAnalyser();
      analyser.fftSize = 256;
      micSource = audioCtx.createMediaStreamSource(stream);
      micSource.connect(analyser);

      const dataArray = new Uint8Array(analyser.frequencyBinCount);
      volumeInterval = window.setInterval(() => {
        if (!analyser) return;
        analyser.getByteFrequencyData(dataArray);
        let sum = 0;
        for (let i = 0; i < dataArray.length; i++) {
          sum += dataArray[i];
        }
        const avg = sum / dataArray.length;
        // Map average volume (0-255) to roughly 0-1
        onVolume(Math.min(1, avg / 128));
      }, 100);
    }

    ws.onopen = () => {
      onLog("Audio WebSocket connected.");
      recorder = new MediaRecorder(stream, { mimeType: "audio/webm" });
      recorder.ondataavailable = (e) => {
        if (e.data.size > 0 && ws.readyState === WebSocket.OPEN) {
          ws.send(e.data);
        }
      };
      // Send chunks every 250ms
      recorder.start(250);
      onLog("Audio capture started.");
    };

    ws.onerror = () => onLog("Audio WebSocket error.", true);
    ws.onclose = () => onLog("Audio WebSocket closed.");

    return {
      stop: () => {
        if (volumeInterval) window.clearInterval(volumeInterval);
        if (micSource) micSource.disconnect();
        if (audioCtx) audioCtx.close();
        
        if (recorder && recorder.state !== "inactive") {
          recorder.stop();
        }
        stream.getTracks().forEach((t) => t.stop());
        if (ws.readyState === WebSocket.OPEN) {
          ws.close();
        }
        onLog("Audio capture stopped.");
      },
    };
  } catch (err) {
    onLog(`Failed to start audio capture: ${err instanceof Error ? err.message : String(err)}`, true);
    throw err;
  }
}
