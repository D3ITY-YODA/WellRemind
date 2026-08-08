// sw.js — Service Worker
// This runs in the background and shows notifications when the server sends a push.

self.addEventListener('install', () => {
  self.skipWaiting(); // Activate immediately
});

self.addEventListener('activate', (event) => {
  event.waitUntil(clients.claim()); // Take control of all open tabs
});

// Fired when a push notification arrives from the server
self.addEventListener('push', (event) => {
  if (!event.data) return;

  const { title, body } = event.data.json();

  const options = {
    body,
    icon: '/icon.svg',
    badge: '/icon.svg',
    vibrate: [200, 100, 200],
    data: { timestamp: Date.now() },
  };

  event.waitUntil(
    self.registration.showNotification(title, options)
  );
});

// Clicking the notification opens or focuses the app
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      if (clientList.length > 0) {
        return clientList[0].focus();
      }
      return clients.openWindow('/');
    })
  );
});

self.addEventListener('message', (event) => {
  if (!event.data || event.data.type !== 'SKIP_WAITING') {
    return;
  }
  self.skipWaiting();
});
