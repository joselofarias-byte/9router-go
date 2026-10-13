import 'material-symbols/outlined.css'
import { mount } from 'svelte'
import App from './App.svelte'
import './index.css'
import { initRuntimeI18n } from './lib/i18n'
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

// Runtime translations are applied after mount and kept in sync with Svelte
// updates by a MutationObserver. The saved server locale is reconciled by App.
void initRuntimeI18n()

export default app
