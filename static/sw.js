const CACHE = 'giverny-mobile-v40';
const SHELL = [
  '/static/style.css',
  '/static/js/app.js',
  '/static/js/cash.min.js',
  '/static/js/ws-client.js',
  '/static/js/kanban.js',
  '/static/js/card-detail.js',
  '/static/js/mobile.js',
  '/static/js/webauthn.js',
  '/static/mobile.css',
  '/static/fa/icons.css',
  '/static/fa/icons.woff2',
  '/static/manifest.webmanifest',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png',
  '/static/icons/icon-maskable-192.png',
  '/static/icons/icon-maskable-512.png'
];
self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(SHELL)));
});
self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(
    keys.filter(key => key !== CACHE).map(key => caches.delete(key))
  )).then(() => self.clients.claim()));
});
self.addEventListener('fetch', event => {
  if (event.request.method === 'GET' && new URL(event.request.url).pathname.startsWith('/static/')) {
    event.respondWith(caches.match(event.request).then(cached => cached || fetch(event.request)));
  }
});
self.addEventListener('push', event => {
  const data = event.data ? event.data.json() : {};
  event.waitUntil(self.registration.showNotification(data.title || 'Giverny', {
    body: data.body || '',
    icon: '/static/icons/icon-192.png',
    data: {url: data.url || '/mobile/'}
  }));
});
self.addEventListener('notificationclick', event => {
  event.notification.close();
  const url = event.notification.data && event.notification.data.url || '/mobile/';
  event.waitUntil(self.clients.matchAll({type: 'window'}).then(clients => {
    for (const client of clients) if (client.url.endsWith(url) && 'focus' in client) return client.focus();
    return self.clients.openWindow(url);
  }));
});
