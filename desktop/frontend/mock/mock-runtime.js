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

// Update simulator mirroring desktop/update_runs.go: one run per game,
// MOCK_SLOTS running at once, the rest in phase 'queued'.
let MOCK_SLOTS = 2
const mockRuns = new Map() // gameID → {cancelled}
let mockRunning = 0
const mockWaiters = []
function mockSlot(run) {
  if (mockRunning < MOCK_SLOTS) { mockRunning++; return Promise.resolve(true) }
  return new Promise((res) => mockWaiters.push({run, res}))
}
function mockFreeSlot() {
  mockRunning--
  while (mockWaiters.length && mockRunning < MOCK_SLOTS) {
    const w = mockWaiters.shift()
    if (w.run.cancelled) { w.res(false); continue }
    mockRunning++
    w.res(true)
  }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
async function mockUpdate(gameID, onDone) {
  const run = {cancelled: false}
  mockRuns.set(gameID, run)
  const g = (typeof UPDATABLE_GAMES !== 'undefined' ? UPDATABLE_GAMES : []).find((x) => x.id === gameID) || {}
  let ok = false
  try {
    if (mockRunning >= MOCK_SLOTS) emit('game-update:phase', {gameID, phase: 'queued'})
    if (!(await mockSlot(run)) || run.cancelled) {
      emit('game-update:error', {gameID, step: 'queued', message: 'Cancelled while queued'})
      return
    }
    try {
      for (const phase of ['syncing', 'selecting-link']) {
        emit('game-update:phase', {gameID, phase}); await sleep(600)
      }
      emit('game-update:phase', {gameID, phase: 'downloading'})
      for (let p = 0; p <= 100; p += 3.3333) {
        if (run.cancelled) { emit('game-update:error', {gameID, step: 'download', message: 'Cancelled'}); return }
        emit('game-update:download-progress', {gameID, percent: Math.min(p, 100), speedBytesPerSec: 4.2e6, bytesDownloaded: p * 1e6, totalBytes: 100e6})
        await sleep(150)
      }
      for (const phase of ['extracting', 'merging', 'updating-db']) {
        emit('game-update:phase', {gameID, phase}); await sleep(500)
      }
      emit('game-update:complete', {gameID, oldVersion: g.version || '0.1', newVersion: g.latestVersion || '0.2'})
      ok = true
    } finally {
      mockFreeSlot()
    }
  } finally {
    mockRuns.delete(gameID)
    onDone?.(ok)
    emit('game-update:idle', {gameID})
  }
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
// Fields the Edit Game dialog can change that don't live on the summary row
// (developer/overview/tags/notes/store links/wine prefix/F95 URL). Kept per
// game so an edit is visible on the next GetGameDetail, like the real DB.
const META_OVERRIDES = new Map()
function overridesFor(id) {
  if (!META_OVERRIDES.has(Number(id))) META_OVERRIDES.set(Number(id), {})
  return META_OVERRIDES.get(Number(id))
}
function detailOf(id) {
  const summary = gameOf(id)
  if (!summary) return null
  const ov = overridesFor(id)
  return {
    ...summary,
    developer: ov.developer ?? summary.developer,
    overview: ov.overview ?? summary.overview,
    coverUrl: `${window.go.main.AppCoverBase()}/cover/${id}/full`,
    f95Url: ov.f95Url ?? `https://f95zone.to/threads/example-${id}.${100000 + id}`,
    tags: ov.tags ?? (GAME_TAGSETS[id] || ['Visual Novel']),
    notes: ov.notes ?? '', storeLinks: ov.storeLinks ?? {}, steamAppId: 0, winePrefix: ov.winePrefix ?? '',
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
  EditGame:            (id, fields = {}) => delay().then(() => {
    const g = gameOf(id)
    if (!g) throw new Error('game with id ' + id + ' not found')
    const ov = overridesFor(id)
    // Mirror desktop/app.go's nil/empty contract: only present keys change.
    if (fields.title != null) g.title = fields.title
    if (fields.engine != null) g.engine = fields.engine
    if (fields.version != null) g.version = fields.version
    if (fields.status != null) g.status = fields.status
    if (fields.exePath != null) g.exePath = fields.exePath
    if (fields.developer != null) ov.developer = fields.developer
    if (fields.overview != null) ov.overview = fields.overview
    if (fields.tags != null) ov.tags = fields.tags
    if (fields.notes != null) ov.notes = fields.notes
    if (fields.storeLinks != null) ov.storeLinks = fields.storeLinks
    if (fields.winePrefix != null) ov.winePrefix = fields.winePrefix
    if (fields.f95Url != null) ov.f95Url = fields.f95Url
  }),
  RenameGame:          () => delay().then(() => {}),
  RemoveGame:          () => delay().then(() => {}),
  RemoveDuplicate:     () => delay().then(() => {}),
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
  DownloadGameUpdate:  (id) => delay().then(() => {
    if (mockRuns.has(id)) throw new Error('an update is already in progress for this game')
    mockUpdate(id)
  }),
  DownloadAllUpdates:  () => delay().then(() => {
    const list = UPDATABLE_GAMES.filter((g) => g.updateState !== 'unknown' && !mockRuns.has(g.id))
    emit('game-update:batch-start', {total: list.length})
    let started = 0, succeeded = 0, failed = 0, left = list.length
    for (const g of list) {
      mockUpdate(g.id, (ok) => {
        ok ? succeeded++ : failed++
        emit('game-update:game-done', {gameID: g.id, title: g.title, success: ok, error: ok ? '' : 'Cancelled'})
        if (--left === 0) setTimeout(() => emit('game-update:batch-complete', {succeeded, failed, total: list.length}), 0)
      })
      started++
    }
  }),
  CancelGameUpdate:    () => delay().then(() => {
    const n = mockRuns.size
    mockRuns.forEach((r) => { r.cancelled = true })
    mockWaiters.splice(0).forEach((w) => w.res(false))
    if (n) emit('game-update:cancelled', {})
    return n > 0
  }),
  CancelGameUpdateFor: (id) => delay().then(() => {
    const r = mockRuns.get(id)
    if (!r) return false
    r.cancelled = true
    const i = mockWaiters.findIndex((w) => w.run === r)
    if (i >= 0) mockWaiters.splice(i, 1)[0].res(false)
    return true
  }),
  GetUpdateConcurrency: () => delay().then(() => MOCK_SLOTS),
  SetUpdateConcurrency: (n) => delay().then(() => { MOCK_SLOTS = n; mockFreeSlot(); mockRunning++ }),
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
  GetCoverArtSettings: () => delay().then(() => ({steam: true, vndb: false, sgdbKeySet: true, sgdbKeyHint: 'a1b2', sgdbFromEnv: false})),
  SetCoverSources:     () => delay().then(() => {}),
  SetSteamGridDBKey:   () => delay().then(() => {}),
  UpgradeCovers:       () => delay().then(() => {
    let i = 0
    const t = setInterval(() => {
      i++
      emit('covers:upgrade-progress', {current: i, total: 5, title: GAMES[i]?.title || '', phase: 'searching'})
      if (i === 5) { clearInterval(t); emit('covers:upgrade-complete', {checked: 5, replaced: 3, failed: 0, errors: ['Some Game: vndb: HTTP 429']}) }
    }, 400)
  }),
  FindCoverCandidates: () => delay(600).then(() => [
    {url: 'https://picsum.photos/seed/a/600/900', w: 600, h: 900, source: 'steam', note: 'Steam library capsule'},
    {url: 'https://picsum.photos/seed/b/660/930', w: 660, h: 930, source: 'steamgriddb', note: 'SteamGridDB grid · score 4'},
    {url: 'https://picsum.photos/seed/c/256/360', w: 256, h: 360, source: 'vndb', note: 'VNDB v1'},
  ]),
  SetGameCover:        () => delay().then(() => {}),
  SetGameCoverFromFile: () => delay().then(() => {}),
  PickCoverImage:      () => delay().then(() => '/home/mili/Pictures/cover.png'),
  PreviewCoverFile:    () => delay().then(() => 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC'),
  SetCoverLocked:      () => delay().then(() => {}),
  RevertCover:         () => delay().then(() => {}),
  OpenUpdateDownloadPage: () => delay().then(() => 'https://pixeldrain.com/u/aQiB1niF'),
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