import { useCallback, useEffect, useRef, useState } from "react";

import { ActivityEvent, openActivitySocket } from "../services/backend";

function eventKey(event: ActivityEvent) {
  return `${event.timestamp}:${event.category}:${event.label}:${event.detail || ""}`;
}

export function useActivityStream() {
  const [events, setEvents] = useState<ActivityEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const shouldReconnect = useRef(true);

  useEffect(() => {
    let reconnectTimer: number | undefined;
    let socket: WebSocket | undefined;
    shouldReconnect.current = true;

    const connect = () => {
      socket = openActivitySocket(
        (event) =>
          setEvents((prev) => {
            const key = eventKey(event);
            if (prev.some((existing) => eventKey(existing) === key)) {
              return prev;
            }
            return [...prev.slice(-99), event];
          }),
        setConnected,
      );

      socket.onclose = () => {
        setConnected(false);
        if (shouldReconnect.current) {
          reconnectTimer = window.setTimeout(connect, 2000);
        }
      };
    };

    connect();

    return () => {
      shouldReconnect.current = false;
      if (reconnectTimer) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);

  const clearEvents = useCallback(() => setEvents([]), []);

  return { events, connected, clearEvents };
}
