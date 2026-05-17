/// <reference types="vite/client" />

interface Window {
  noteit?: {
    minimize: () => Promise<{ ok: boolean }>;
    close: () => Promise<{ ok: boolean }>;
    openExternal: (url: string) => Promise<{ ok: boolean }>;
  };
}
