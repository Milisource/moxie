<script>
  import {onMount, tick} from 'svelte'
  import {fly} from 'svelte/transition'
  import {EventsOn} from '../wailsjs/runtime/runtime'
  import {GetGames, GetVersion, GetStartupError, ListDeletedGames, RestoreGame, PurgeDeleted, GetCookieStatus, SyncAllGames, DownloadGameUpdate, DownloadAllUpdates, CancelGameUpdate, CancelSync, ProvideUpdateFile, ScanDirectory, FetchCovers, GetGameCount, CheckForUpdate, DownloadUpdate, ApplyUpdate, InstallGame} from '../wailsjs/go/main/App'
  import Sidebar from './lib/Sidebar.svelte'
  import GameList from './lib/GameList.svelte'
  import GameDetail from './lib/GameDetail.svelte'
  import ScanDialog from './lib/ScanDialog.svelte'
  import GameUpdatesView from './lib/GameUpdatesView.svelte'
  import DownloadsView from './lib/DownloadsView.svelte'
  import AddGameDialog from './lib/AddGameDialog.svelte'
  import SyncDialog from './lib/SyncDialog.svelte'
  import F95Browser from './lib/F95Browser.svelte'
  import DedupDialog from './lib/DedupDialog.svelte'
  import CollectionsView from './lib/CollectionsView.svelte'
  import CoversView from './lib/CoversView.svelte'
  import SettingsView from './lib/SettingsView.svelte'
  import StatusBar from './lib/StatusBar.svelte'
  import {library, appMeta, setLastSyncAt} from './lib/viewState.svelte.js'

  let version = $state('')
  let games = $state([])
  let statusMsg = $state('Ready')
  let activeView = $state('library')
  let lastUpdate = $state(0)
  let selectedGameId = $state(null)
  let loading = $state(true)
  let gameListRef = $state.raw()   // bound to GameList; exposes focusSearch() for the global shortcut below
  let deletedGames = $state([])
  // Non-empty when the backend failed to start (usually the database). Without
  // this every bound call just answers "database not initialized" and the user
  // sees an empty library with no explanation.
  let startupError = $state('')

  // Sync state lives here (App level) rather than inside SyncDialog so it
  // survives tab switches. SyncDialog used to own its state and event
  // subscriptions, so navigating away destroyed them: an in-flight sync
  // vanished from the UI, its completion result was lost, and returning to
  // the tab allowed starting a second concurrent run.
  let syncState = $state({
    cookieStatus: '',        // 'available' | 'not_found' | ''
    syncing: false,
    progress: {current: 0, total: 0, title: '', phase: ''},
    gameResults: [],         // {id, title, status, version}
    result: null,            // {associated, updated, skipped, errors} or null
    syncError: '',
  })

  // Cover backfill state lives here for the same reason: CoversView is
  // destroyed on tab switch, and an in-flight fetch must stay visible (and
  // non-redundant) from any view.
  let coverState = $state({
    gameCount: null,           // null = not loaded
    fetching: false,
    progress: {current: 0, total: 0, title: '', phase: ''},
    result: null,              // {fetched, failed, skipped, total, backfilled} or null
    coverError: '',
  })

  // ── App self-update state (App level) ─────────────────
  // The DownloadUpdate pipeline runs in the background. UpdateDialog used to
  // own this state and its update:* subscriptions, so leaving the Settings
  // tab mid-download killed the progress UI and reset the button while the
  // backend kept downloading unseen. State and subscriptions live here so the
  // whole check→download→apply flow survives tab switches.
  let appUpdateState = $state({
    checking: false,
    info: null,              // CheckForUpdate result or null
    downloading: false,
    downloadProgress: {downloaded: 0, total: 0},
    downloadComplete: false,
    error: '',
  })

  async function checkAppUpdate() {
    if (appUpdateState.checking || appUpdateState.downloading) return
    appUpdateState.checking = true
    appUpdateState.error = ''
    appUpdateState.info = null
    appUpdateState.downloadComplete = false
    appUpdateState.downloadProgress = {downloaded: 0, total: 0}
    try {
      const result = await CheckForUpdate()
      // CheckForUpdate carries API failures in `error` instead of throwing —
      // surface it rather than rendering "up to date".
      if (result?.error) {
        appUpdateState.error = result.error
        appUpdateState.info = null
      } else {
        appUpdateState.info = result
      }
    } catch (e) {
      appUpdateState.error = String(e)
    }
    appUpdateState.checking = false
  }

  async function downloadAppUpdate() {
    if (appUpdateState.downloading) return
    appUpdateState.downloading = true
    appUpdateState.error = ''
    appUpdateState.downloadComplete = false
    appUpdateState.downloadProgress = {downloaded: 0, total: 0}
    try {
      await DownloadUpdate()
    } catch (e) {
      appUpdateState.error = String(e)
      appUpdateState.downloading = false
    }
  }

  async function applyAppUpdate() {
    try {
      await ApplyUpdate()
    } catch (e) {
      appUpdateState.error = String(e)
    }
  }

  // ── Install pipeline state (App level) ─────────────────
  // InstallGame shares the update single-run lock with the game-update
  // pipeline and runs in the background. GameDetail used to own this state
  // and its game-install:* subscriptions, so navigating back to the library
  // mid-install reset the Install button to its default while the backend
  // kept going (and a second click could start a redundant second run).
  /** @type {{running:boolean, gameId:number|null, phase:string, progress:number, error:string}} */
  let installState = $state({running: false, gameId: null, phase: '', progress: 0, error: ''})

  async function startInstall(gameId, dest) {
    if (installState.running) return
    installState = {running: true, gameId, phase: 'selecting-link', progress: 0, error: ''}
    try {
      await InstallGame(gameId, dest)
    } catch (e) {
      installState = {...installState, running: false, phase: 'error', error: String(e)}
    }
  }

  function syncPhaseLabel(phase) {
    if (phase === 'associating') return 'Associating games'
    if (phase === 'checking-updates') return 'Checking for updates'
    return 'Synchronizing'
  }

  async function startSync(force = false) {
    if (syncState.syncing) return   // UI guard — the backend single-flight rejects a 2nd run anyway
    syncState.syncError = ''
    syncState.result = null
    syncState.gameResults = []
    syncState.progress = {current: 0, total: 0, title: '', phase: ''}
    syncState.syncing = true
    try {
      await SyncAllGames(force)
    } catch (e) {
      syncState.syncError = String(e)
      syncState.syncing = false
    }
  }

  // Ask the backend to stop the running sync. The backend emits the usual
  // completion events; this also flips the local flag so the UI never stays
  // stuck in "Syncing…" if no event follows the cancel.
  async function cancelSync() {
    if (!syncState.syncing) return
    try {
      await CancelSync()
    } catch (e) {
      syncState.syncError = String(e)
    }
    syncState.syncing = false
  }

  // ── Scan state (App level) ─────────────────────────────────
  // Moved up from ScanDialog, exactly like syncState: the backend scan runs
  // in a goroutine, so navigating away mid-scan must not destroy the state
  // or the event subscriptions — otherwise the UI loses the running flag and
  // lets the user start a second concurrent scan.
  let scanState = $state({
    scanning: false,
    currentPath: '',
    progress: {dirsExamined: 0, gamesFound: 0, phase: ''},
    showProgress: false,
    lastResult: null,            // { gamesFound, inserted, updated, errors } or null
    scanError: '',
  })

  async function startScan(path, force = false) {
    if (scanState.scanning) return   // prevent duplicate concurrent runs
    scanState.scanning = true
    scanState.currentPath = path
    scanState.showProgress = true
    scanState.lastResult = null
    scanState.scanError = ''
    scanState.progress = {dirsExamined: 0, gamesFound: 0, phase: ''}
    try {
      await ScanDirectory(path, force)
    } catch (e) {
      scanState.scanError = String(e)
      scanState.scanning = false
    }
  }

  // ── Game update pipeline state (App level) ─────────────────
  // Like syncState, this lives here so the Updates view keeps its state (and
  // the backend's single-flight pipeline stays visible) when switching tabs
  // mid-run. The update view just renders this state and calls the callbacks.
  /** @type {Record<number, {phase:string,percent:number,speed:number,bytesDownloaded:number,totalBytes:number,filesExtracted:number,totalFiles:number,currentFile:string,error:string,oldVersion:string,newVersion:string}>} */
  let gameStates = $state({})
  /** @type {{running:boolean,retrying?:boolean,current:number,total:number,currentGameTitle:string,results:Array,error:string,succeeded?:number,failed?:number}|null} */
  let batchState = $state(null)
  // Sequential retry plumbing: the backend pipeline is single-flight, so
  // retries run one at a time. retryInFlight tracks which game the backend
  // is currently pumping; game-update:idle (empty payload, fires after every
  // pipeline) tells us that game finished.
  let retryQueue = $state([])
  let retryInFlight = $state(null)

  const UPDATE_BUSY_PHASES = ['syncing', 'selecting-link', 'downloading', 'extracting', 'merging', 'updating-db']

  // True when ANY update/install pipeline holds the backend single-run lock —
  // the global truth every action (Updates view, detail update/install)
  // respects, since updates and installs share that lock.
  let pipelineBusy = $derived(
    installState.running ||
    !!batchState?.running ||
    Object.values(gameStates).some(s => s && UPDATE_BUSY_PHASES.includes(s.phase))
  )

  // One-line description of the running pipeline for the status bar, so a
  // background update/install stays visible from any tab.
  let pipelineLabel = $derived.by(() => {
    if (installState.running) {
      const g = games.find(x => Number(x.id) === installState.gameId)
      return `Installing ${g?.title || 'game'}…`
    }
    if (batchState?.running) {
      return batchState.retrying
        ? `Retrying updates… (${batchState.current}/${batchState.total})`
        : `Updating ${batchState.current} of ${batchState.total} games…`
    }
    const busyId = Object.keys(gameStates).find(id => {
      const s = gameStates[id]
      return s && UPDATE_BUSY_PHASES.includes(s.phase)
    })
    if (busyId) {
      const g = games.find(x => Number(x.id) === Number(busyId))
      return `Updating ${g?.title || 'game'}…`
    }
    return ''
  })

  // Surfaced in the status bar (P2 item 11 — "free space for ... upgrade
  // suggests") once the user has manually checked from Settings; this app
  // never auto-checks on startup, so there's no extra network call here.
  let updateAvailable = $derived(
    !appUpdateState.downloading && appUpdateState.info?.hasUpdate ? appUpdateState.info : null
  )

  function updateGS(gameId, patch) {
    gameStates = {...gameStates, [gameId]: {...(gameStates[gameId] || {}), ...patch}}
  }

  // A pipeline that dies before its per-game events arrive leaves rows in the
  // optimistic 'syncing' phase (startUpdateAll) — isUpdatingAny stays true
  // forever and every row/button stays disabled. Reset busy phases back to
  // idle and drop any phantom gameStates[0] entry the backend may have left.
  function resetStaleBusyPhases() {
    const next = {...gameStates}
    let changed = false
    if (next[0]) {
      delete next[0]
      changed = true
    }
    for (const [id, gs] of Object.entries(next)) {
      if (gs && UPDATE_BUSY_PHASES.includes(gs.phase)) {
        next[id] = {...gs, phase: 'idle'}
        changed = true
      }
    }
    if (changed) gameStates = next
  }

  // Cancel everything the UI knows about. Mirrors the backend's
  // game-update:cancelled handling so rows can never wedge in a busy phase.
  function markAllAsCancelled() {
    const next = {...gameStates}
    for (const [id, gs] of Object.entries(next)) {
      // 'selecting-file' is not a busy phase but is still non-terminal —
      // a cancel must never leave a row wedged in it.
      if (UPDATE_BUSY_PHASES.includes(gs.phase) || gs.phase === 'selecting-file') {
        next[id] = {...gs, phase: 'error', error: 'Cancelled'}
      }
    }
    gameStates = next
    if (batchState?.running) batchState = {...batchState, running: false, error: 'Cancelled'}
  }

  function retryTitle(gameId) {
    const g = games.find(g => Number(g.id) === Number(gameId))
    if (g?.title) return g.title
    const r = (batchState?.results || []).find(r => Number(r.gameID) === Number(gameId))
    return r?.title || ''
  }

  // Single-game update (row button / per-row retry). The backend runs the
  // pipeline in a goroutine and rejects a second concurrent run — surface
  // that rejection instead of dropping it.
  function startUpdateGame(gameId) {
    const gs = gameStates[gameId] || {phase: 'idle'}
    if (gs.phase !== 'idle' && gs.phase !== 'error') return
    updateGS(gameId, {phase: 'syncing', percent: 0, speed: 0, bytesDownloaded: 0, totalBytes: 0, filesExtracted: 0, totalFiles: 0, currentFile: '', error: '', oldVersion: '', newVersion: '', manualRequired: false, manualHost: '', step: ''})
    DownloadGameUpdate(gameId).catch((e) => {
      const msg = String(e)
      if (batchState?.retrying && /already in progress/i.test(msg)) {
        // Retry Failed was clicked in the tiny window between batch-complete
        // and the backend releasing its single-run lock. Hold this game at
        // the front of the queue — the next game-update:idle (the lock
        // release) pumps it for real.
        updateGS(gameId, {phase: 'error', error: msg})
        retryInFlight = null
        retryQueue = [gameId, ...retryQueue]
        return
      }
      updateGS(gameId, {phase: 'error', error: msg})
      if (batchState?.retrying && retryInFlight !== null) {
        // The pipeline never started (e.g. a hard backend error) and no idle
        // will follow — record the failure and move on so the pass can't stall.
        const failedId = retryInFlight
        retryInFlight = null
        batchState = {
          ...batchState,
          results: [...batchState.results, {
            gameID: failedId,
            title: retryTitle(failedId),
            success: false,
            error: msg,
          }],
        }
        pumpRetryQueue()
      }
    })
  }

  // Manual fallback for auto-download failures (Cloudflare-blocked hosts,
  // dead links). The backend opens a native file picker for the archive the
  // user downloaded by hand, then resumes the pipeline from extraction. It
  // returns an error only if the pipeline could not be started; per-game
  // failures surface via the game-update:* events.
  async function provideUpdateFile(gameId) {
    const id = Number(gameId)
    try {
      await ProvideUpdateFile(id)
    } catch (e) {
      updateGS(id, {phase: 'error', error: String(e)})
    }
  }

  async function startUpdateAll(updatableGames) {
    if (batchState?.running) return
    const list = updatableGames || []
    // Optimistically mark every updatable game as syncing until the backend
    // confirms the batch actually started.
    const newStates = {...gameStates}
    for (const game of list) {
      newStates[game.id] = {phase: 'syncing', percent: 0, speed: 0, bytesDownloaded: 0, totalBytes: 0, filesExtracted: 0, totalFiles: 0, currentFile: '', error: '', oldVersion: '', newVersion: ''}
    }
    gameStates = newStates
    batchState = {running: true, current: 0, total: list.length, currentGameTitle: '', results: [], error: ''}
    try {
      await DownloadAllUpdates()
    } catch (e) {
      batchState = {...batchState, running: false, error: String(e)}
      // The batch never started (e.g. the backend's single-run CAS rejected
      // it because a single-game update holds the lock). Roll back the
      // optimistic 'syncing' phases we set above, or isUpdatingAny stays true
      // forever and every row spins with no pipeline behind it.
      resetStaleBusyPhases()
    }
  }

  // Retry the failed games of the last batch, one at a time. The backend
  // won't emit batch-start/progress/complete for this path, so we keep a
  // live batchState ourselves and accumulate per-game results from the
  // game-update:complete/error events (via the idle correlation below).
  function handleRetryFailed() {
    const failed = (batchState?.results || []).filter(r => !r.success)
    if (failed.length === 0) return
    batchState = {
      running: true,
      retrying: true,
      current: 0,
      total: failed.length,
      currentGameTitle: '',
      results: [],
      error: '',
      succeeded: 0,
      failed: 0,
    }
    retryQueue = failed.map(f => f.gameID)
    pumpRetryQueue()
  }

  function pumpRetryQueue() {
    if (!batchState?.running) return        // cancelled / already finished
    if (retryQueue.length === 0) {
      finishRetryBatch()
      return
    }
    const [next, ...rest] = retryQueue
    retryQueue = rest
    retryInFlight = next
    batchState = {
      ...batchState,
      current: batchState.total - retryQueue.length,
      currentGameTitle: retryTitle(next),
    }
    startUpdateGame(next)
  }

  function finishRetryBatch() {
    if (!batchState?.running || !batchState?.retrying) return
    const results = batchState.results
    const succeeded = results.filter(r => r.success).length
    const failed = results.filter(r => !r.success).length
    batchState = {...batchState, running: false, retrying: false, succeeded, failed}
    refreshGames()
    lastUpdate++
  }

  function cancelUpdates() {
    retryQueue = []
    retryInFlight = null
    CancelGameUpdate().then((cancelled) => {
      // false = nothing was running — the backend emits no
      // game-update:cancelled event, so run the same phase reset locally to
      // make sure stale busy phases can't wedge the UI.
      if (!cancelled) markAllAsCancelled()
    }).catch(() => {
      // Binding rejected (e.g. "already in progress" race): still reset, a
      // stuck row is worse than a redundant cancel attempt.
      markAllAsCancelled()
    })
  }

  // Like startSync: UI guard plus backend single-flight (coverRunning) as
  // the backstop. State survives tab switches because it lives here.
  async function startCoverFetch() {
    if (coverState.fetching) return
    coverState.coverError = ''
    coverState.result = null
    coverState.progress = {current: 0, total: 0, title: '', phase: ''}
    coverState.fetching = true
    try {
      await FetchCovers()
    } catch (e) {
      coverState.coverError = String(e)
      coverState.fetching = false
    }
  }

  async function loadGames() {
    loading = true
    try {
      games = await GetGames()
      // No count here — the status bar's own game-count span (StatusBar.svelte)
      // already shows it persistently; repeating it in the transient message
      // was both redundant and read like a console log line ("N games loaded").
      statusMsg = 'Library ready'
    } catch (e) {
      statusMsg = `Couldn't load your library — ${e}`
    }
    loading = false
  }

  // View-switch animation parameters. Tab switches fade+rise the incoming
  // view (~140ms); users with reduced-motion preference get a hard switch
  // (0ms, no transform) so nothing flashes or drifts. Evaluated once at
  // module load — the preference doesn't change while the app is running.
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  function viewMotion() {
    if (reducedMotion) return {y: 0, duration: 0}
    return {y: 8, duration: 140}
  }

  async function init() {
    try { version = await GetVersion() } catch (e) { version = '?' }

    // Check before loading games — if startup failed, every data call will
    // fail too, and the banner explains why rather than a bare status message.
    try { startupError = await GetStartupError() } catch (e) { startupError = '' }
    if (startupError) {
      statusMsg = 'Startup failed'
      loading = false
      return
    }

    await loadGames()

    // Cookie status gates the sync view; load it once here so the tab opens
    // with the state already known.
    try { syncState.cookieStatus = await GetCookieStatus() } catch (e) { syncState.cookieStatus = '' }

    try { coverState.gameCount = await GetGameCount() } catch (e) { coverState.gameCount = 0 }
  }

  function openDetail(id) {
    selectedGameId = id
    activeView = 'detail'
  }

  function closeDetail() {
    selectedGameId = null
    activeView = 'library'
  }

  async function refreshGames() {
    loading = true
    try {
      games = await GetGames()
    } catch (e) {
      // Callers own their status message — setting a generic "N games loaded"
      // here would clobber meaningful results (scan/sync/cover summaries).
    } finally {
      loading = false
    }
  }

  // A game added from the F95Zone browser: refresh the library (new rows,
  // count in the status bar) and bump lastUpdate so sidebar badges (updates,
  // counts) reflect the new library.
  async function handleBrowserGameAdded() {
    await refreshGames()
    lastUpdate++
  }

  async function loadTrash() {
    try { deletedGames = await ListDeletedGames() } catch (e) { console.error(e) }
  }

  $effect(() => {
    if (activeView === 'trash') loadTrash()
  })

  async function handleRestore(id) {
    try {
      await RestoreGame(id)
      await loadTrash()
      await refreshGames()
      statusMsg = 'Game restored'
    } catch (e) { statusMsg = `Couldn't restore that game — ${e}` }
  }

  async function handlePurge() {
    if (!confirm(`Permanently delete ${deletedGames.length} games?`)) return
    try {
      await PurgeDeleted()
      await loadTrash()
      await refreshGames()
      statusMsg = 'Trash emptied'
    } catch (e) { statusMsg = `Couldn't empty the trash — ${e}` }
  }

  let unsubAutoScan
  let unsubAutoScanError
  let unsubAutoScanStarted
  let unsubAutoScanProgress
  let unsubSyncProgress
  let unsubSyncGameDone
  let unsubSyncComplete
  let unsubSyncError
  let unsubCoversProgress
  let unsubCoversComplete
  let unsubCoversError
  let unsubScanProgress
  let unsubScanComplete
  let unsubScanError
  let unsubGamePhase
  let unsubGameDownload
  let unsubGameExtract
  let unsubGameError
  let unsubGameManual
  let unsubGameComplete
  let unsubGameBatchStart
  let unsubGameBatchProgress
  let unsubGameDone
  let unsubGameBatchComplete
  let unsubGameCancelled
  let unsubGameIdle
  let unsubAppUpdateProgress
  let unsubAppUpdateComplete
  let unsubAppUpdateError
  let unsubInstallPhase
  let unsubInstallProgress
  let unsubInstallError
  let unsubInstallComplete

  // Global search shortcut (P1 item 8): "/" or Ctrl/Cmd+F jumps to the
  // library and focuses its search field, Steam/Playnite-style. "/" is only
  // intercepted when nothing is already accepting text input, so it doesn't
  // clobber typing elsewhere (e.g. the F95 browser's own search box).
  function isTypingTarget(el) {
    if (!el) return false
    const tag = el.tagName
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
  }

  function handleGlobalKeydown(e) {
    const isSlash = e.key === '/' && !isTypingTarget(e.target)
    const isFindCombo = (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f'
    if (!isSlash && !isFindCombo) return
    e.preventDefault()
    activeView = 'library'
    // GameList is destroyed/remounted on tab switch (see the {#if} below), so
    // its search field may not exist yet on this same tick.
    tick().then(() => gameListRef?.focusSearch())
  }

  onMount(() => {
    init()
    window.addEventListener('keydown', handleGlobalKeydown)
    // Live library refresh when the directory watcher finishes an auto-scan.
    unsubAutoScan = EventsOn('scan:auto-complete', async (r) => {
      let msg = ''
      if (r) {
        const parts = []
        if (r.inserted) parts.push(`${r.inserted} new`)
        if (r.updated) parts.push(`${r.updated} updated`)
        if (r.removed) parts.push(`${r.removed} removed`)
        msg = parts.length
          ? `Auto-scan: ${parts.join(', ')}`
          : 'Auto-scan: no changes'
      }
      try {
        await refreshGames()
        // refreshGames sets its own "Library ready" message — restore the
        // auto-scan result afterwards so the user actually sees it.
        if (msg) statusMsg = msg
      } catch (e) {
        statusMsg = `Auto-scan finished, but the library refresh failed — ${e}`
      }
    })
    unsubAutoScanError = EventsOn('scan:auto-error', (r) => {
      statusMsg = `Auto-scan failed — ${r?.error || 'unknown error'}`
    })
    // The directory watcher's auto-scans emit these but nothing displayed
    // them — surface them in the status bar so background scanning is
    // visible from any tab.
    unsubAutoScanStarted = EventsOn('scan:auto', () => {
      statusMsg = 'Auto-scan in progress…'
    })
    unsubAutoScanProgress = EventsOn('scan:auto-progress', (r) => {
      statusMsg = `Auto-scan in progress… (${r?.dirsExamined ?? 0} dirs, ${r?.gamesFound ?? 0} games)`
    })
    // Sync events are tracked at app level so the sync view keeps its state
    // (and the status bar keeps reporting progress) no matter which tab is
    // active. SyncDialog just renders this state.
    unsubSyncProgress = EventsOn('sync:progress', (data) => {
      // A live run clears any stale rejection text (e.g. a double-click
      // that hit the "already running" guard before the button disabled).
      syncState.syncError = ''
      syncState.progress = data
      syncState.syncing = true
      statusMsg = `Sync: ${syncPhaseLabel(data?.phase)} (${data?.current ?? 0}/${data?.total ?? 0})`
    })
    unsubSyncGameDone = EventsOn('sync:game-done', (data) => {
      // A sync run cached this game's cover — retry it right away (app-level
      // so it also fires while the library tab is hidden, see covers:complete).
      if (data?.id && library.failedCovers.has(data.id)) {
        library.coverEpoch++
        const next = new Set(library.failedCovers)
        next.delete(data.id)
        library.failedCovers = next
      }
      syncState.gameResults = [...syncState.gameResults, data].slice(-50)
    })
    unsubSyncComplete = EventsOn('sync:complete', async (data) => {
      syncState.result = data
      syncState.syncing = false
      try {
        // A sync run may have cached covers or associated new games — refresh
        // so cover cells and rows reflect the new state.
        await refreshGames()
        // A sync can discover new game versions: bump lastUpdate so the
        // updates view (and sidebar badge) refresh even while it's open.
        lastUpdate++
        setLastSyncAt(new Date().toISOString())
        statusMsg = 'Sync complete — library refreshed'
      } catch (e) {
        statusMsg = `Sync finished, but the library refresh failed — ${e}`
      }
    })
    unsubSyncError = EventsOn('sync:error', (data) => {
      syncState.syncError = data?.error || 'Sync failed'
      syncState.syncing = false
      statusMsg = `Sync error: ${syncState.syncError}`
    })
    // Manual scan events live at app level too (like sync): the backend scan
    // runs in a goroutine, so navigating away must not drop the running flag
    // or the completion result.
    unsubScanProgress = EventsOn('scan:progress', (data) => {
      scanState.progress = data
    })
    unsubScanComplete = EventsOn('scan:complete', (data) => {
      scanState.lastResult = data
      scanState.scanning = false
      refreshGames()
    })
    unsubScanError = EventsOn('scan:error', (data) => {
      scanState.scanError = data.error || 'Unknown error'
      scanState.scanning = false
    })
    // Game update pipeline events (App level so they survive tab switches).
    unsubGamePhase = EventsOn('game-update:phase', (data) => {
      updateGS(data.gameID, {phase: data.phase})
    })
    unsubGameDownload = EventsOn('game-update:download-progress', (data) => {
      updateGS(data.gameID, {
        percent: Math.min(data.percent ?? 0, 100),
        speed: data.speedBytesPerSec ?? 0,
        bytesDownloaded: data.bytesDownloaded ?? 0,
        totalBytes: data.totalBytes ?? 0,
      })
    })
    unsubGameExtract = EventsOn('game-update:extract-progress', (data) => {
      updateGS(data.gameID, {
        filesExtracted: data.filesExtracted ?? 0,
        totalFiles: data.totalFiles ?? 0,
        currentFile: data.currentFile ?? '',
      })
    })
    unsubGameManual = EventsOn('game-update:manual-required', (data) => {
      // Auto-download failed (Cloudflare-blocked host, dead link, …). The
      // pipeline has already ended in the error state; flag the game so the
      // UI can offer the file-picker fallback (ProvideUpdateFile).
      updateGS(data.gameID, {
        manualRequired: true,
        manualHost: data.host || '',
      })
    })
    unsubGameError = EventsOn('game-update:error', (data) => {
      // A gameID of 0 (or the 'list-updatable' step) means the whole batch
      // failed before it started (e.g. GetUpdatableGames error). The backend
      // emits this instead of batch-complete, so treat it as a batch-level
      // failure — otherwise batchState.running stays true forever and the UI
      // is stuck with a spinner and disabled buttons.
      if (data.gameID === 0 || data.step === 'list-updatable') {
        batchState = {
          ...(batchState || {running: true, current: 0, total: Math.max(games.length, 1), currentGameTitle: '', results: [], error: ''}),
          running: false,
          error: data.message || 'Update check failed',
        }
        // startUpdateAll optimistically marks every game 'syncing' before the
        // batch starts; with the batch never starting those states would keep
        // isUpdatingAny true and leave every button disabled. Reset active
        // phases back to idle (and drop the phantom gameStates[0]) so the
        // rows and action buttons re-enable.
        resetStaleBusyPhases()
        return
      }
      updateGS(data.gameID, {phase: 'error', error: data.message || 'Unknown error', step: data.step || ''})
    })
    unsubGameComplete = EventsOn('game-update:complete', (data) => {
      updateGS(data.gameID, {
        phase: 'done',
        oldVersion: data.oldVersion || '',
        newVersion: data.newVersion || '',
      })
      // Refresh once per pipeline, not once per game: during a batch (or a
      // retry pass) batchState stays set and the summary handlers below do a
      // single refresh; only a standalone single-game update refreshes here.
      if (!batchState) {
        refreshGames()
        lastUpdate++
      }
    })
    unsubGameBatchStart = EventsOn('game-update:batch-start', (data) => {
      batchState = {
        running: true,
        current: 0,
        total: data.total || 0,
        currentGameTitle: '',
        results: [],
        error: '',
      }
    })
    unsubGameBatchProgress = EventsOn('game-update:batch-progress', (data) => {
      if (batchState) {
        batchState = {...batchState, current: data.current ?? 0, currentGameTitle: data.currentGameTitle || ''}
      }
    })
    unsubGameDone = EventsOn('game-update:game-done', (data) => {
      if (batchState) {
        batchState = {
          ...batchState,
          results: [...batchState.results, {
            gameID: data.gameID,
            title: data.title || '',
            success: !!data.success,
            error: data.error || '',
          }],
        }
      }
    })
    unsubGameBatchComplete = EventsOn('game-update:batch-complete', (data) => {
      if (batchState) {
        batchState = {
          ...batchState,
          running: false,
          succeeded: data.succeeded ?? 0,
          failed: data.failed ?? 0,
        }
        // One refresh for the whole batch (not N per-game refreshes).
        refreshGames()
        lastUpdate++
      }
    })
    unsubGameCancelled = EventsOn('game-update:cancelled', () => {
      markAllAsCancelled()
    })
    // The backend releases its single-run lock and emits this after EVERY
    // pipeline (single update, batch, install). It carries no gameID, so the
    // only pipeline we can attribute it to is the current sequential retry —
    // record its outcome (phase was already set by complete/error) and start
    // the next one. This is what chains retries one-at-a-time.
    unsubGameIdle = EventsOn('game-update:idle', () => {
      if (retryInFlight !== null) {
        const gs = gameStates[retryInFlight] || {}
        const ok = gs.phase === 'done'
        if (batchState) {
          batchState = {
            ...batchState,
            results: [...batchState.results, {
              gameID: retryInFlight,
              title: retryTitle(retryInFlight),
              success: ok,
              error: ok ? '' : (gs.error || 'Update failed'),
            }],
          }
        }
        retryInFlight = null
      }
      // Always pump: also covers the lock-race path where startUpdateGame was
      // rejected while the previous pipeline still held the lock and the game
      // was re-queued.
      pumpRetryQueue()
    })
    // Cover backfill events: live at App level so the state (and the
    // in-flight run) survives tab switches. After completion, newly cached
    // covers need hasCover=true to render at all — refresh the library.
    unsubCoversProgress = EventsOn('covers:progress', (r) => {
      coverState.progress = r
      coverState.fetching = true
    })
    unsubCoversComplete = EventsOn('covers:complete', async (r) => {
      coverState.result = r
      coverState.fetching = false
      // Failed-cover retry bookkeeping lives here (not in GameList) because
      // this subscription survives tab switches: a backfill finishing while
      // the library tab is hidden must still bump the epoch so the next
      // mount re-requests those covers instead of re-rendering cached 404s.
      if (library.failedCovers.size > 0) {
        library.coverEpoch++
        library.failedCovers = new Set()
      }
      try {
        await refreshGames()
        if (r?.total === 0) statusMsg = 'All games already have covers'
        else statusMsg = `Cover fetch complete — ${r?.fetched ?? 0} cached`
      } catch (e) {
        statusMsg = `Covers fetched, but the library refresh failed — ${e}`
      }
    })
    unsubCoversError = EventsOn('covers:error', (r) => {
      coverState.coverError = r?.error || 'Cover fetch failed'
      coverState.fetching = false
    })
    // App self-update flow events (App level so the download keeps its UI
    // across tab switches — the backend pipeline runs in the background).
    unsubAppUpdateProgress = EventsOn('update:progress', (data) => {
      appUpdateState.downloadProgress = data || appUpdateState.downloadProgress
    })
    unsubAppUpdateComplete = EventsOn('update:complete', () => {
      appUpdateState.downloading = false
      appUpdateState.downloadComplete = true
    })
    unsubAppUpdateError = EventsOn('update:error', (data) => {
      appUpdateState.error = data?.error || 'Update failed'
      appUpdateState.downloading = false
    })
    // Install pipeline events (App level so the install keeps its UI across
    // navigation; GameDetail just renders installState). InstallGame shares
    // the update lock, so the shell must also know when it is running.
    unsubInstallPhase = EventsOn('game-install:phase', (data) => {
      if (Number(data?.gameID) === Number(installState.gameId)) {
        installState = {...installState, phase: data.phase || ''}
      }
    })
    unsubInstallProgress = EventsOn('game-install:download-progress', (data) => {
      if (Number(data?.gameID) === Number(installState.gameId)) {
        installState = {...installState, progress: Math.round(data.percent ?? 0)}
      }
    })
    unsubInstallError = EventsOn('game-install:error', (data) => {
      if (Number(data?.gameID) === Number(installState.gameId)) {
        installState = {...installState, running: false, phase: 'error', error: data?.message || 'Install failed'}
        refreshGames()
      }
    })
    unsubInstallComplete = EventsOn('game-install:complete', (data) => {
      if (Number(data?.gameID) === Number(installState.gameId)) {
        installState = {...installState, running: false, phase: 'done', progress: 100}
        refreshGames()
        lastUpdate++
      }
    })
    return () => {
      window.removeEventListener('keydown', handleGlobalKeydown)
      if (unsubAutoScan) unsubAutoScan()
      if (unsubAutoScanError) unsubAutoScanError()
      if (unsubAutoScanStarted) unsubAutoScanStarted()
      if (unsubAutoScanProgress) unsubAutoScanProgress()
      if (unsubSyncProgress) unsubSyncProgress()
      if (unsubSyncGameDone) unsubSyncGameDone()
      if (unsubSyncComplete) unsubSyncComplete()
      if (unsubSyncError) unsubSyncError()
      if (unsubScanProgress) unsubScanProgress()
      if (unsubScanComplete) unsubScanComplete()
      if (unsubScanError) unsubScanError()
      if (unsubGamePhase) unsubGamePhase()
      if (unsubGameDownload) unsubGameDownload()
      if (unsubGameExtract) unsubGameExtract()
      if (unsubGameError) unsubGameError()
      if (unsubGameManual) unsubGameManual()
      if (unsubGameComplete) unsubGameComplete()
      if (unsubGameBatchStart) unsubGameBatchStart()
      if (unsubGameBatchProgress) unsubGameBatchProgress()
      if (unsubGameDone) unsubGameDone()
      if (unsubGameBatchComplete) unsubGameBatchComplete()
      if (unsubGameCancelled) unsubGameCancelled()
      if (unsubGameIdle) unsubGameIdle()
      if (unsubCoversProgress) unsubCoversProgress()
      if (unsubCoversComplete) unsubCoversComplete()
      if (unsubCoversError) unsubCoversError()
      if (unsubAppUpdateProgress) unsubAppUpdateProgress()
      if (unsubAppUpdateComplete) unsubAppUpdateComplete()
      if (unsubAppUpdateError) unsubAppUpdateError()
      if (unsubInstallPhase) unsubInstallPhase()
      if (unsubInstallProgress) unsubInstallProgress()
      if (unsubInstallError) unsubInstallError()
      if (unsubInstallComplete) unsubInstallComplete()
    }
  })
</script>

<div class="shell">
  <Sidebar {version} bind:activeView onNavigate={(id) => activeView = id} {lastUpdate}/>

  <main class="main">
    {#if startupError}
      <div class="startup-error">
        <h2>Moxie could not start</h2>
        <p class="startup-error-msg">{startupError}</p>
        <p class="startup-error-hint">
          The library is unavailable until this is resolved. Check that the config
          directory is writable and that no other copy of Moxie is running.
        </p>
      </div>
    {:else}
      <!-- Keyed by the active view: the old view animates out while the new
           one animates in. Long-running state (sync/scan/covers/updates) lives
           above this block, so it survives the remounts either way. -->
      {#key activeView}
        <div class="view" transition:fly={viewMotion()}>
          {#if activeView === 'detail' && selectedGameId !== null}
            <GameDetail
              gameId={selectedGameId}
              onBack={closeDetail}
              onUpdate={refreshGames}
              gameState={gameStates[selectedGameId]}
              pipelineBusy={pipelineBusy}
              installState={installState}
              onUpdateGame={startUpdateGame}
              onProvideFile={provideUpdateFile}
              onInstall={startInstall}
            />
          {:else if activeView === 'library'}
            <GameList
              bind:this={gameListRef}
              {games}
              {loading}
              {gameStates}
              onOpenDetail={openDetail}
              onUpdate={refreshGames}
              onLaunched={(msg) => statusMsg = msg}
            />
          {:else if activeView === 'scan'}
            <ScanDialog
              scanning={scanState.scanning}
              currentPath={scanState.currentPath}
              progress={scanState.progress}
              showProgress={scanState.showProgress}
              lastResult={scanState.lastResult}
              scanError={scanState.scanError}
              onScan={startScan}
            />
          {:else if activeView === 'settings'}
            <SettingsView
              appVersion={version}
              appUpdateState={appUpdateState}
              onCheckAppUpdate={checkAppUpdate}
              onDownloadAppUpdate={downloadAppUpdate}
              onApplyAppUpdate={applyAppUpdate}
            />
          {:else if activeView === 'updates'}
            <GameUpdatesView
              gameStates={gameStates}
              batchState={batchState}
              {lastUpdate}
              installRunning={installState.running}
              onNavigate={(id) => activeView = id}
              onUpdateGame={startUpdateGame}
              onUpdateAll={startUpdateAll}
              onRetryFailed={handleRetryFailed}
              onProvideFile={provideUpdateFile}
              onCancel={cancelUpdates}
            />
          {:else if activeView === 'downloads'}
            <DownloadsView />
          {:else if activeView === 'covers'}
            <CoversView
              gameCount={coverState.gameCount}
              fetching={coverState.fetching}
              progress={coverState.progress}
              result={coverState.result}
              coverError={coverState.coverError}
              onFetch={startCoverFetch}
            />
          {:else if activeView === 'add'}
            <AddGameDialog onGameAdded={refreshGames}/>
          {:else if activeView === 'sync'}
            <SyncDialog
              cookieStatus={syncState.cookieStatus}
              syncing={syncState.syncing}
              progress={syncState.progress}
              gameResults={syncState.gameResults}
              result={syncState.result}
              syncError={syncState.syncError}
              onSync={startSync}
              onCancel={cancelSync}
            />
          {:else if activeView === 'browser'}
            <F95Browser onAdded={handleBrowserGameAdded}/>
          {:else if activeView === 'collections'}
            <CollectionsView
              onOpenDetail={openDetail}
              onCollectionsChanged={() => lastUpdate++}
            />
          {:else if activeView === 'duplicates'}
            <DedupDialog onDedupDone={refreshGames}/>
          {:else if activeView === 'trash'}
            <div class="trash-view">
              <h2>Trash</h2>
              {#if deletedGames.length === 0}
                <p class="text-muted">No deleted games.</p>
              {:else}
                <div class="trash-actions">
                  <button class="btn btn-danger" onclick={handlePurge}>Purge All ({deletedGames.length})</button>
                </div>
                <div class="table-header">
                  <span>Title</span><span>Engine</span><span>Actions</span>
                </div>
                {#each deletedGames as game (game.id)}
                  <div class="table-row">
                    <span>{game.title}</span>
                    <span>{game.engine || '—'}</span>
                    <span>
                      <button class="btn btn-sm" onclick={() => handleRestore(game.id)}>Restore</button>
                    </span>
                  </div>
                {/each}
              {/if}
            </div>
          {:else}
            <div class="placeholder-view">
              <h2 style="text-transform: capitalize">{activeView}</h2>
              <p>Coming soon.</p>
            </div>
          {/if}
        </div>
      {/key}
    {/if}

    <StatusBar
      {statusMsg}
      gameCount={games.length}
      {pipelineBusy}
      {pipelineLabel}
      appUpdateDownloading={appUpdateState.downloading}
      lastSyncAt={appMeta.lastSyncAt}
      {updateAvailable}
      onGoToUpdate={() => activeView = 'settings'}
    />
  </main>
</div>

<style>
  .shell {
    display: flex;
    height: 100vh;
    width: 100vw;
  }

  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;   /* keep the status bar pinned to the bottom */
    min-width: 0;
    background: var(--bg-primary);
    position: relative;
  }

  /* Keyed view container. Absolutely positioned so the outgoing and incoming
     views overlap during the 140ms switch instead of sharing the flex row —
     with both in normal flow (flex:1 each) the screen would split 50/50 and
     the incoming view would render squished. bottom: 25px keeps the view
     above the 24px status bar (plus its 1px border). */
  .view {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 25px;
    display: flex;
    flex-direction: column;
  }

  .placeholder-view {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    flex: 1;
    gap: 8px;
    color: var(--text-secondary);
  }
  .placeholder-view h2 { font-size: 18px; font-weight: 600; }
  .placeholder-view p  { font-size: 14px; }

  .startup-error {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 32px;
    text-align: center;
  }
  .startup-error h2 { font-size: 18px; font-weight: 700; color: var(--danger); margin: 0; }
  .startup-error-msg {
    font-size: 13px;
    font-family: var(--font-mono);
    color: var(--text-primary);
    max-width: 560px;
    margin: 0;
  }
  .startup-error-hint {
    font-size: 13px;
    color: var(--text-secondary);
    max-width: 560px;
    margin: 0;
  }

  .trash-view { flex: 1; overflow: auto; padding: 32px; max-width: 720px; margin: 0 auto; width: 100%; }
  .trash-view h2 { font-size: 20px; font-weight: 700; margin: 0 0 16px; }
  .trash-actions { margin-bottom: 16px; }
  .trash-view .table-header {
    display: grid;
    grid-template-columns: 1fr 110px 100px;
    gap: 8px;
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border-bottom: 1px solid var(--border);
    background: var(--bg-tertiary);
  }
  .trash-view .table-row {
    display: grid;
    grid-template-columns: 1fr 110px 100px;
    gap: 8px;
    padding: 8px 12px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
    align-items: center;
  }
  .trash-view .table-row:hover { background: var(--bg-hover); }
  .btn-sm { padding: 4px 10px; font-size: 12px; cursor: pointer; border: 1px solid var(--border); border-radius: 6px; background: transparent; color: var(--text-primary); }
  .btn-sm:hover { background: var(--bg-hover); }
  .btn-danger { background: var(--danger); color: #fff; border: none; padding: 7px 16px; border-radius: 6px; font-size: 13px; cursor: pointer; }
  .btn-danger:hover { opacity: 0.9; }
  .text-muted { color: var(--text-muted); font-size: 14px; }
</style>
