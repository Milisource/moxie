// Dev-only entry point for standalone-browser runs (screenshots, styling).
// Loads the mock Wails runtime BEFORE the app mounts, then boots the real app.
import '../mock/mock-runtime.js'
import {mount} from 'svelte'
import './app.css'
import App from './App.svelte'

mount(App, {
  target: document.getElementById('app'),
})