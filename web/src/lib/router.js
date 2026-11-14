import { useEffect, useState } from 'react';

// A two-route history router: "/" (overview) and "/m/<id>" (machine detail).
export function usePath() {
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const on = () => setPath(window.location.pathname);
    window.addEventListener('popstate', on);
    return () => window.removeEventListener('popstate', on);
  }, []);
  return path;
}

export function navigate(to) {
  if (to === window.location.pathname) return;
  window.history.pushState(null, '', to);
  window.dispatchEvent(new PopStateEvent('popstate'));
  window.scrollTo(0, 0);
}

export function machinePath(id) {
  return `/m/${encodeURIComponent(id)}`;
}

export function matchMachine(path) {
  const m = path.match(/^\/m\/([^/]+)\/?$/);
  return m ? decodeURIComponent(m[1]) : null;
}

// onLinkClick lets plain clicks on <a href> use the router while keeping
// middle-click / cmd-click opening a new tab.
export function onLinkClick(e) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  navigate(e.currentTarget.getAttribute('href'));
}

// useTitle names the browser tab after the page: "web-1 · sshud", or just "sshud".
export function useTitle(title) {
  useEffect(() => {
    document.title = title ? `${title} · sshud` : 'sshud';
  }, [title]);
}
