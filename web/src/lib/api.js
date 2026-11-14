import { useEffect, useState } from 'react';

export function useJSON(url) {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    let alive = true;
    fetch(url)
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.statusText))))
      .then((d) => {
        if (!alive) return;
        setData(d);
        setError(null);
      })
      .catch((e) => alive && setError(e));
    return () => {
      alive = false;
    };
  }, [url]);
  return { data, error };
}

// A stream the server refused (401 after the session expired, for example)
// is closed for good by the browser; App re-checks the login when told.
let streamClosed = () => {};
export const onStreamClosed = (fn) => {
  streamClosed = fn;
};

// useStream subscribes to /api/stream and returns the latest update per
// machine. The stream is closed while the tab is hidden (so nothing is
// probed for nobody) and reopened, with an immediate frame, when it is shown.
export function useStream(query) {
  const [updates, setUpdates] = useState({});
  const [lost, setLost] = useState(false);

  useEffect(() => {
    let es = null;
    let timer = null;
    let delay = 2000;
    const open = () => {
      if (es) return;
      es = new EventSource(`/api/stream?${query}`);
      es.addEventListener('update', (ev) => {
        const u = JSON.parse(ev.data);
        setLost(false);
        delay = 2000;
        setUpdates((prev) => {
          // keep the last numbers on screen while a reconnect is in progress
          if (u.status === 'connecting' && prev[u.machine]?.snapshot) return prev;
          return { ...prev, [u.machine]: u };
        });
      });
      es.onerror = () => {
        setLost(true);
        if (es.readyState !== EventSource.CLOSED) return; // the browser retries by itself
        es = null;
        streamClosed();
        // still logged in (the server restarted, say): reopen, backing off
        timer = setTimeout(() => !document.hidden && open(), delay);
        delay = Math.min(delay * 2, 60000);
      };
    };
    const close = () => {
      clearTimeout(timer);
      es?.close();
      es = null;
    };
    const onVisibility = () => (document.hidden ? close() : open());
    if (!document.hidden) open();
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      close();
    };
  }, [query]);

  return { updates, lost };
}

// api sends a JSON request and returns the decoded body, throwing the
// server's error message (and its kind, if any) on failure.
export async function api(method, path, body) {
  const r = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = r.status === 204 ? null : await r.json().catch(() => null);
  if (!r.ok) throw Object.assign(new Error(data?.error || r.statusText), { kind: data?.kind });
  return data;
}

export function retry(id) {
  return fetch(`/api/machines/${encodeURIComponent(id)}/retry`, { method: 'POST' });
}
