#!/usr/bin/env node
// Screenshot a page in headless Chrome with a real mobile/desktop viewport and
// locale, via the DevTools protocol. `--window-size` alone cannot go below
// 500 px wide on macOS, so this sets device metrics explicitly.
// Needs a global WebSocket: Node 22+, or Node 20.10+ where this script
// re-runs itself with --experimental-websocket. No npm dependencies.
//
//   node scripts/shot.mjs <url> <out.png> [--width 390] [--height 844]
//        [--lang zh-CN] [--scheme dark|light] [--wait 3000] [--full]
//        [--eval "js run after --wait"] [--after 1500]
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

if (typeof WebSocket === 'undefined') {
  const r = spawnSync(process.execPath, ['--experimental-websocket', ...process.argv.slice(1)], { stdio: 'inherit' });
  process.exit(r.status ?? 1);
}

const [url, out, ...rest] = process.argv.slice(2);
if (!url || !out) {
  console.error('usage: shot.mjs <url> <out.png> [--width N] [--height N] [--lang L] [--scheme dark|light] [--wait ms] [--full]');
  process.exit(2);
}
const opt = { width: 1440, height: 900, lang: 'en-US', scheme: 'dark', wait: 3000, full: false, eval: '', after: 1500 };
for (let i = 0; i < rest.length; i++) {
  const k = rest[i].replace(/^--/, '');
  if (k === 'full') opt.full = true;
  else if (k === 'eval') opt.eval = rest[++i];
  else opt[k] = /^\d+$/.test(rest[i + 1]) ? Number(rest[++i]) : rest[++i];
}

const chrome = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const port = 9300 + Math.floor(Math.random() * 500);
const proc = spawn(chrome, [
  '--headless=new',
  `--remote-debugging-port=${port}`,
  `--user-data-dir=${mkdtempSync(join(tmpdir(), 'shot-'))}`,
  '--hide-scrollbars',
  'about:blank',
], { stdio: 'ignore' });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function target() {
  for (let i = 0; i < 50; i++) {
    try {
      const list = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
      const page = list.find((t) => t.type === 'page');
      if (page) return page.webSocketDebuggerUrl;
    } catch {}
    await sleep(100);
  }
  throw new Error('chrome did not start');
}

const ws = new WebSocket(await target());
await new Promise((r) => ws.addEventListener('open', r, { once: true }));
let seq = 0;
const pending = new Map();
ws.addEventListener('message', (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.id && pending.has(msg.id)) {
    pending.get(msg.id)(msg);
    pending.delete(msg.id);
  }
});
const send = (method, params = {}) =>
  new Promise((resolve, reject) => {
    const id = ++seq;
    pending.set(id, (m) => (m.error ? reject(new Error(`${method}: ${m.error.message}`)) : resolve(m.result)));
    ws.send(JSON.stringify({ id, method, params }));
  });

try {
  const mobile = opt.width < 600;
  await send('Emulation.setDeviceMetricsOverride', { width: opt.width, height: opt.height, deviceScaleFactor: mobile ? 2 : 1, mobile });
  await send('Emulation.setLocaleOverride', { locale: opt.lang });
  await send('Network.enable');
  await send('Network.setUserAgentOverride', { userAgent: 'Mozilla/5.0 shot', acceptLanguage: opt.lang });
  await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: opt.scheme }] });
  await send('Page.enable');
  await send('Page.navigate', { url });
  await sleep(opt.wait);
  if (opt.eval) {
    // drive the page (open a dialog, fill a form) before the screenshot
    await send('Runtime.evaluate', { expression: opt.eval, awaitPromise: true });
    await sleep(opt.after);
  }
  let clip;
  if (opt.full) {
    const { contentSize } = await send('Page.getLayoutMetrics');
    clip = { x: 0, y: 0, width: opt.width, height: Math.ceil(contentSize.height), scale: 1 };
  }
  const { data } = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: opt.full, ...(clip && { clip }) });
  writeFileSync(out, Buffer.from(data, 'base64'));
  const { result } = await send('Runtime.evaluate', { expression: 'innerWidth + " " + navigator.language + " overflowX=" + (document.documentElement.scrollWidth > innerWidth)', returnByValue: true });
  const { result: title } = await send('Runtime.evaluate', { expression: 'document.title', returnByValue: true });
  console.log(`${out}: ${result.value}${opt.eval ? ` title=${title.value}` : ''}`);
} finally {
  await send('Browser.close').catch(() => {});
  ws.close();
  proc.unref();
}
