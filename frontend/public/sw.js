const VERSION = 'webnix-network-only-v1';

self.addEventListener('install', () => {
  // WebNix is an authenticated online terminal. Do not cache authenticated
  // navigations: doing so could resurrect the app shell after sign-out.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.map((key) => caches.delete(key))))
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) return;

  // Keep a fetch handler for installability, but remain strictly network-only.
  event.respondWith(fetch(event.request));
});

void VERSION;
