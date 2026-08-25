import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import './style.css';

function mustElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) throw new Error(`Missing required UI element: ${selector}`);
  return element;
}

const terminalNode = mustElement<HTMLDivElement>('#terminal');
const statusNode = mustElement<HTMLDivElement>('#status');
const toolbar = mustElement<HTMLElement>('#toolbar');

if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => undefined);
  });
}

const terminal = new Terminal({
  cursorBlink: true,
  convertEol: true,
  scrollback: 8000,
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
  fontSize: 14,
  lineHeight: 1.08,
  theme: {
    background: '#09090b',
    foreground: '#f4f4f5',
    cursor: '#f4f4f5',
    cursorAccent: '#09090b',
    selectionBackground: '#3f3f46',
  },
});

const fit = new FitAddon();
terminal.loadAddon(fit);
terminal.open(terminalNode);

try {
  const webgl = new WebglAddon();
  webgl.onContextLoss(() => webgl.dispose());
  terminal.loadAddon(webgl);
} catch {
  // Canvas renderer is the safe fallback.
}

fit.fit();
terminal.focus();
const browserShortcutsToTerminal = new Set(['p', 'r', 'l', 'f', 's', 'w', 't', 'n']);
terminal.attachCustomKeyEventHandler((event) => {
  const key = event.key.toLowerCase();
  if (event.ctrlKey && !event.metaKey && !event.altKey && browserShortcutsToTerminal.has(key)) {
    event.preventDefault();
    return true;
  }
  return true;
});

const resizeObserver = new ResizeObserver(() => {
  requestAnimationFrame(() => fit.fit());
});
resizeObserver.observe(terminalNode);

let socket: WebSocket | null = null;
let reconnectAttempt = 0;
let reconnectTimer: number | undefined;
let ctrlArmed = false;
let closedByPage = false;

function setStatus(text: string, state: 'connecting' | 'ready' | 'error' = 'connecting') {
  statusNode.textContent = text;
  statusNode.dataset.state = state;
}

function showLoginHint() {
  const login = loginURL();
  if (!login) return;
  setStatus('Not connected. Click here to sign in.', 'error');
  statusNode.role = 'button';
  statusNode.tabIndex = 0;
  statusNode.onclick = () => window.location.assign(login);
  statusNode.onkeydown = (event) => {
    if (event.key === 'Enter' || event.key === ' ') window.location.assign(login);
  };
}

function clearLoginHint() {
  statusNode.removeAttribute('role');
  statusNode.removeAttribute('tabindex');
  statusNode.onclick = null;
  statusNode.onkeydown = null;
}

const terminalOrigin = (import.meta.env.VITE_TERMINAL_ORIGIN as string | undefined)?.replace(/\/$/, '');
const loginReturnURL = `${window.location.origin}${window.location.pathname}?auth=done`;

function wsURL(): string {
  const base = terminalOrigin || window.location.origin;
  const url = new URL('/api/terminal', base);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

function loginURL(): string | null {
  if (!terminalOrigin) return null;
  const url = new URL('/oauth2/start', terminalOrigin);
  url.searchParams.set('rd', loginReturnURL);
  return url.toString();
}


function sendResize() {
  if (!socket || socket.readyState !== WebSocket.OPEN) return;
  socket.send(JSON.stringify({ type: 'resize', cols: terminal.cols, rows: terminal.rows }));
}

terminal.onResize(sendResize);
terminal.onData((data) => {
  if (!socket || socket.readyState !== WebSocket.OPEN) return;
  let payload = data;
  if (ctrlArmed && payload.length === 1) {
    const code = payload.toUpperCase().charCodeAt(0);
    if (code >= 64 && code <= 95) payload = String.fromCharCode(code - 64);
    ctrlArmed = false;
    const button = toolbar.querySelector<HTMLButtonElement>('[data-mod="ctrl"]');
    if (button) button.dataset.active = 'false';
  }
  socket.send(new TextEncoder().encode(payload));
});

toolbar.addEventListener('click', (event) => {
  const button = (event.target as HTMLElement).closest<HTMLButtonElement>('button');
  if (!button) return;
  if (button.dataset.mod === 'ctrl') {
    ctrlArmed = !ctrlArmed;
    button.dataset.active = String(ctrlArmed);
    terminal.focus();
    return;
  }
  const special = button.dataset.special;
  if (special === 'escape') terminal.input('\x1b', true);
  if (special === 'tab') terminal.input('\t', true);
  const key = button.dataset.key;
  if (key) terminal.input(key, true);
  terminal.focus();
});

async function consumeMessage(event: MessageEvent) {
  if (typeof event.data === 'string') {
    try {
      const message = JSON.parse(event.data) as { type?: string; message?: string };
      if (message.type === 'status' && message.message) setStatus(message.message);
      if (message.type === 'ready') setStatus('NixOS workspace connected', 'ready');
      if (message.type === 'error') setStatus(message.message ?? 'Workspace error', 'error');
    } catch {
      terminal.write(event.data);
    }
    return;
  }

  if (event.data instanceof Blob) {
    terminal.write(new Uint8Array(await event.data.arrayBuffer()));
    return;
  }

  if (event.data instanceof ArrayBuffer) terminal.write(new Uint8Array(event.data));
}

function scheduleReconnect() {
  if (closedByPage) return;
  const delay = Math.min(15000, 500 * 2 ** reconnectAttempt) + Math.floor(Math.random() * 250);
  reconnectAttempt += 1;
  window.clearTimeout(reconnectTimer);
  reconnectTimer = window.setTimeout(connect, delay);
  setStatus(`Disconnected. Reconnecting in ${Math.ceil(delay / 1000)}s…`, 'error');
}

function connect() {
  window.clearTimeout(reconnectTimer);
  setStatus('Connecting to NixOS workspace…');

  const ws = new WebSocket(wsURL());
  ws.binaryType = 'arraybuffer';
  socket = ws;

  ws.addEventListener('open', () => {
    clearLoginHint();
    reconnectAttempt = 0;
    sendResize();
  });
  ws.addEventListener('message', (event) => void consumeMessage(event));
  ws.addEventListener('close', (event) => {
    if (terminalOrigin && reconnectAttempt >= 1) showLoginHint();
    scheduleReconnect();
  });
  ws.addEventListener('error', () => {
    if (terminalOrigin) showLoginHint();
    else setStatus('Terminal connection error', 'error');
  });
}

window.addEventListener('beforeunload', () => {
  closedByPage = true;
  socket?.close(1000, 'page unload');
});

connect();
