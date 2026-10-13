import 'material-symbols/outlined.css'
import { mount } from 'svelte'
import App from './App.svelte'
import './index.css'
// 9router is the local gateway itself, not an installable web app. Retire any
// service worker left by older dashboard builds so browsers stop treating the
// localhost dashboard as a PWA and stale app-shell caches disappear.
if ('serviceWorker' in navigator) {
  void navigator.serviceWorker.getRegistrations().then((registrations) => {
    for (const registration of registrations) {
      void registration.unregister()
    }
  }).catch(() => {})
}
if ('caches' in window) {
  void caches.keys().then((keys) =>
    Promise.all(keys.filter((key) => key.startsWith('9router-go-')).map((key) => caches.delete(key)))
  ).catch(() => {})
}


const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
