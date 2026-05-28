/// <reference types="vite/client" />

interface Window {
  novotranscriber?: {
    minimize: () => Promise<{ ok: boolean }>;
    close: () => Promise<{ ok: boolean }>;
    openExternal: (url: string) => Promise<{ ok: boolean }>;
    startCapture: (args: {
      capture_session_id: string;
      client_token: string;
    }) => Promise<{ ok: boolean; pid?: number }>;
    stopCapture: () => Promise<{ ok: boolean }>;
    onCaptureLog: (
      callback: (payload: {
        level: "info" | "error";
        message: string;
        timestamp: number;
      }) => void,
    ) => () => void;
  };
}
