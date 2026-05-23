// sw.js — Service Worker
// This runs in the background and shows notifications when the server sends a push.

self.addEventListener('install', () => {
  self.skipWaiting(); // Activate immediately
});

self.addEventListener('activate', (event) => {
  event.waitUntil(clients.claim()); // Take control of all open tabs
});
