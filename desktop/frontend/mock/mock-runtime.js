// ─────────────────────────────────────────────────────────────
// Mock Wails runtime + backend bindings for standalone browser use.
// Replaces window.go / window.runtime so the Svelte app (which calls the
// generated bindings in wailsjs/) runs against fixture data. Dev-only.
// ─────────────────────────────────────────────────────────────

import {
  GAMES, GAME_DETAILS, GAME_TAGSETS, COLLECTIONS, COLLECTION_GAMES,
  DOWNLOAD_LINKS, DELETED_GAMES, DUPLICATES, SEARCH_RESULTS, THREAD_PREVIEW,
  SCAN_PATHS, PLAY_HISTORY, INSTALL_TARGETS, COOKIE_STATUS, APP_VERSION,
  GAME_COUNT, UPDATABLE_COUNT, UPDATABLE_GAMES,
} from './mock-data.js'

// ── Minimal event bus (wails runtime Events*) ────────────────
const listeners = new Map()

function on(event, cb) {
  if (!listeners.has(event)) listeners.set(event, new Set())
  listeners.get(event).add(cb)
  return () => listeners.get(event)?.delete(cb)
}
function emit(event, ...args) {
  listeners.get(event)?.forEach((cb) => {
    try { cb(...args) } catch (e) { console.error('mock event handler error', e) }
  })
}

window.runtime = {
  EventsOn: on,
  EventsOnMultiple: (event, cb) => on(event, cb),
  EventsOnce: (event, cb) => {
    const off = on(event, (...a) => { cb(...a); off() })
    return off
  },
  EventsOff: (event) => listeners.delete(event),
  EventsOffAll: () => listeners.clear(),
  EventsEmit: emit,
  LogPrint: console.log, LogTrace: () => {}, LogDebug: console.debug,
  LogInfo: console.info, LogWarning: console.warn, LogError: console.error,
  BrowserOpenURL: (url) => console.log('[mock] open URL', url),
  ClipboardGetText: () => '', ClipboardSetText: (t) => t,
  WindowSetTitle: () => {}, WindowSetDarkTheme: () => {}, WindowSetLightTheme: () => {},
  WindowReload: () => location.reload(), Quit: () => {},
  Environment: () => ({platform: 'linux', arch: 'amd64'}),
}

// ── Backend bindings (window.go.main.App) ────────────────────
// Every generated binding returns a Promise; mirror that. Methods the UI
// calls on mount must return sane fixtures; mutations resolve silently.
const delay = (ms = 15) => new Promise((r) => setTimeout(r, ms))

// ?empty=1 → empty library (empty-state screenshots)
const EMPTY = new URLSearchParams(location.search).has('empty')

// ?stress=N → pad the library to N games (clones of the fixtures with
// unique ids/titles) for virtualization/keyboard-nav verification against a
// realistic-size library — see docs/desktop-perf-virtualization-handoff.md
// and the Phase 6 keyboard-nav verification, both of which exercise this
// against ~400+ games.
const STRESS = Number(new URLSearchParams(location.search).get('stress')) || 0
const STRESS_GAMES = STRESS > GAMES.length
  ? Array.from({length: STRESS}, (_, i) => {
      const base = GAMES[i % GAMES.length]
      return {...base, id: 100000 + i, title: `${base.title} #${i + 1}`}
    })
  : GAMES

function gameOf(id) {
  return STRESS_GAMES.find((g) => g.id === Number(id)) ?? GAMES.find((g) => g.id === Number(id))
}
function detailOf(id) {
  const summary = gameOf(id)
  if (!summary) return null
  return {
    ...summary,
    developer: summary.developer,
    overview: summary.overview,
    coverUrl: `${window.go.main.AppCoverBase()}/cover/${id}/full`,
    f95Url: `https://f95zone.to/threads/example-${id}.${100000 + id}`,
    tags: GAME_TAGSETS[id] || ['Visual Novel'],
    notes: '', storeLinks: {}, steamAppId: 0, winePrefix: '',
    downloadLinks: DOWNLOAD_LINKS.slice(0, 2).map((l) => ({...l, id: l.id + id})),
    playHistory: PLAY_HISTORY,
  }
}
function searchGames(q) {
  const needle = (q || '').toLowerCase()
  if (!needle) return []
  return GAMES.filter((g) =>
    g.title.toLowerCase().includes(needle) ||
    g.engine.toLowerCase().includes(needle)
  )
}

const App = {
  // ── Info / env
  // ?empty=1 loads an empty library (for empty-state screenshots)
  GetVersion:          () => delay().then(() => APP_VERSION),
  GetStartupError:     () => delay().then(() => ''),
  GetConfigDir:        () => delay().then(() => '/home/mili/.config/moxie'),
  GetDbPath:           () => delay().then(() => '/home/mili/.config/moxie/games.db'),
  GetCookieStatus:     () => delay().then(() => COOKIE_STATUS),
  GetCoverBaseURL:     () => delay(1).then(() => `${location.origin}/mock/covers`),
  CheckDependencies:   () => delay().then(() => ({ok: true})),
  // Shape mirrors desktop/app.go's UpdateInfo (hasUpdate/currentVersion/
  // latestVersion/releaseUrl) — UpdateDialog.svelte reads exactly those keys.
  CheckForUpdate:      () => delay().then(() => ({
    hasUpdate: true, currentVersion: APP_VERSION, latestVersion: 'v0.4.1',
    releaseUrl: 'https://github.com/example/moxie/releases',
  })),
  DownloadUpdate:      () => delay().then(() => {}),
  ApplyUpdate:         () => delay().then(() => {}),

  // ── Games
  GetGames:            () => delay().then(() => (EMPTY ? [] : STRESS_GAMES)),
  GetGameCount:        () => delay().then(() => (EMPTY ? 0 : GAME_COUNT)),
  GetGameDetail:       (id) => delay().then(() => detailOf(id)),
  SearchGames:         (q) => delay(80).then(() => searchGames(q)),
  AddGame:             () => delay().then(() => ({id: 999})),
  AddGameFromF95Zone:  () => delay().then(() => ({id: 999})),
  EditGame:            () => delay().then(() => {}),
  RenameGame:          () => delay().then(() => {}),
  RemoveGame:          () => delay().then(() => {}),
  RestoreGame:         () => delay().then(() => {}),
  PurgeDeleted:        () => delay().then(() => {}),
  ListDeletedGames:    () => delay().then(() => DELETED_GAMES),
  DetectGame:          () => delay().then(() => ({engine: 'Unity', version: '1.0.0'})),
  SetGameStatus:       () => delay().then(() => {}),
  SetGameWinePrefix:   () => delay().then(() => {}),
  // Mirrors desktop/app.go PlayGame: rejects virtual (/virtual/) games and
  // records a play-history entry, so the library's recency arrangement can
  // be demoed (a launched game bubbles to the top of "recently played").
  PlayGame:            (id) => delay().then(() => {
    const g = gameOf(id)
    if (!g) throw new Error('game with id ' + id + ' not found')
    if (g.path?.startsWith('/virtual/')) {
      throw new Error(`"${g.title}" was added from F95Zone but not yet downloaded. Use Install on its detail page to download it.`)
    }
    g.lastPlayed = new Date().toISOString()
    return `Launching ${g.title}`
  }),

  // ── Collections
  // coverIds mirrors desktop/app.go's GetCollections: first COLLAGE_LIMIT
  // member games with a cover, in COLLECTION_GAMES order.
  GetCollections:      () => delay().then(() => (EMPTY ? [] : COLLECTIONS.map((c) => ({
    ...c,
    coverIds: (COLLECTION_GAMES[c.id] || [])
      .map((gid) => gameOf(gid))
      .filter((g) => g?.hasCover)
      .slice(0, 4)
      .map((g) => g.id),
  })))),
  GetCollectionGames:  (id) => delay().then(() => (COLLECTION_GAMES[id] || []).map((gid) => gameOf(gid))),
  GetGameCollections:  () => delay().then(() => [{gameId: 1, collectionIds: [1, 2]}]),
  CreateCollection:    (name) => delay().then(() => ({id: 99, name, gameCount: 0})),
  DeleteCollection:    () => delay().then(() => {}),
  AddGameToCollection: () => delay().then(() => {}),
  RemoveGameFromCollection: () => delay().then(() => {}),

  // ── Updates
  GetUpdatableCount:   () => delay().then(() => (EMPTY ? 0 : UPDATABLE_COUNT)),
  GetUpdatableGames:   () => delay().then(() => (EMPTY ? [] : UPDATABLE_GAMES)),
  DownloadGameUpdate:  () => delay().then(() => {}),
  DownloadAllUpdates:  () => delay().then(() => {}),
  CancelGameUpdate:    () => delay().then(() => {}),
  GetGameDownloadLinksForUpdate: (id) => delay().then(() => DOWNLOAD_LINKS),
  ProvideUpdateFile:   () => delay().then(() => {}),

  // ── Downloads
  GetGamesWithDownloadLinks: () => delay().then(() => GAMES.slice(0, 6).map((g, i) => ({
    ...DOWNLOAD_LINKS[i % DOWNLOAD_LINKS.length],
    id: 500 + g.id, // unique per game so {#each} keys don't collide
    gameId: g.id, gameTitle: g.title, gamePath: g.path,
  }))),
  GetAllDownloadLinks: () => delay().then(() => DOWNLOAD_LINKS),
  GetGameDownloadLinks: (id) => delay().then(() => DOWNLOAD_LINKS),
  OpenDownloadURL:     () => delay().then(() => {}),
  GetInstallTargets:   () => delay().then(() => INSTALL_TARGETS),
  InstallGame:         () => delay().then(() => {}),

  // ── Scan / covers / sync
  GetScanPaths:        () => delay().then(() => SCAN_PATHS),
  AddScanPath:         () => delay().then(() => {}),
  RemoveScanPath:      () => delay().then(() => {}),
  ScanDirectory:       () => delay().then(() => ({gamesFound: 0, inserted: 0, updated: 0, errors: 0})),
  RescanDirectory:     () => delay().then(() => ({gamesFound: 0})),
  PickDirectory:       () => delay().then(() => '/home/mili/Games'),
  FetchCovers:         () => delay().then(() => {}),
  FindDuplicateGames:  () => delay().then(() => DUPLICATES),
  SyncAllGames:        () => delay().then(() => {}),
  SyncSingleGame:      () => delay().then(() => {}),
  CancelSync:          () => delay().then(() => {}),

  // ── F95Zone browser
  SearchF95Zone:       (q) => delay(120).then(() => SEARCH_RESULTS),
  GetThreadPreview:    () => delay(60).then(() => THREAD_PREVIEW),
}

// Covers are served from /mock/covers/<id>/thumb|full (vite plugin) —
// expose a helper the mock cover URLs use to point back at the server.
window.go = {
  main: {
    App,
    AppCoverBase: () => `${location.origin}/mock/covers`,
  },
}