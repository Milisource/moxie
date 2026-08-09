<script>
  import {onMount} from 'svelte'
  import {EventsOn} from '../wailsjs/runtime/runtime'
  import {GetGames, GetVersion, GetStartupError, ListDeletedGames, RestoreGame, PurgeDeleted, GetCookieStatus, SyncAllGames, DownloadGameUpdate, DownloadAllUpdates, CancelGameUpdate, ScanDirectory, FetchCovers, GetGameCount} from '../wailsjs/go/main/App'
  import Sidebar from './lib/Sidebar.svelte'
  import GameList from './lib/GameList.svelte'
  import GameDetail from './lib/GameDetail.svelte'
  import ScanDialog from './lib/ScanDialog.svelte'
  import UpdateDialog from './lib/UpdateDialog.svelte'
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

  let version = $state('')
  let games = $state([])
  let statusMsg = $state('Ready')
  let activeView = $state('library')
  let lastUpdate = $state(0)
  let selectedGameId = $state(null)
  let loading = $state(true)
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

  async function startScan(path) {
    if (scanState.scanning) return   // prevent duplicate concurrent runs
    scanState.scanning = true
    scanState.currentPath = path
    scanState.showProgress = true
    scanState.lastResult = null
    scanState.scanError = ''
    scanState.progress = {dirsExamined: 0, gamesFound: 0, phase: ''}
    try {
      await ScanDirectory(path)
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

  function updateGS(gameId, patch) {
    gameStates = {...gameStates, [gameId]: {...(gameStates[gameId] || {}), ...patch}}
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
    updateGS(gameId, {phase: 'syncing', percent: 0, speed: 0, bytesDownloaded: 0, totalBytes: 0, filesExtracted: 0, totalFiles: 0, currentFile: '', error: '', oldVersion: '', newVersion: ''})
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
    CancelGameUpdate().catch(() => { /* nothing running */ })
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
      statusMsg = `${games.length} game${games.length !== 1 ? 's' : ''} loaded`
    } catch (e) {
      statusMsg = `Error: ${e}`
    }
    loading = false
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
    try {
      games = await GetGames()
      statusMsg = `${games.length} game${games.length !== 1 ? 's' : ''} loaded`
    } catch (e) {
      statusMsg = `Error: ${e}`
    }
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
    } catch (e) { statusMsg = `Error: ${e}` }
  }

  async function handlePurge() {
    if (!confirm(`Permanently delete ${deletedGames.length} games?`)) return
    try {
      await PurgeDeleted()
      await loadTrash()
      await refreshGames()
      statusMsg = 'Trash emptied'
    } catch (e) { statusMsg = `Error: ${e}` }
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
  let unsubGameComplete
  let unsubGameBatchStart
  let unsubGameBatchProgress
  let unsubGameDone
  let unsubGameBatchComplete
  let unsubGameCancelled
  let unsubGameIdle
  onMount(() => {
    init()
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
        // refreshGames sets its own "N games loaded" message — restore the
        // auto-scan result afterwards so the user actually sees it.
        if (msg) statusMsg = msg
      } catch (e) {
        statusMsg = `Error: ${e}`
      }
    })
    unsubAutoScanError = EventsOn('scan:auto-error', (r) => {
      statusMsg = `Auto-scan error: ${r?.error || 'unknown'}`
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
      syncState.gameResults = [...syncState.gameResults, data]
    })
    unsubSyncComplete = EventsOn('sync:complete', async (data) => {
      syncState.result = data
      syncState.syncing = false
      try {
        // A sync run may have cached covers or associated new games — refresh
        // so cover cells and rows reflect the new state.
        await refreshGames()
        statusMsg = 'Sync complete — library refreshed'
      } catch (e) {
        statusMsg = `Error: ${e}`
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
        // Drop any phantom per-game entry a previous mishap may have written.
        if (gameStates[0]) {
          const cleaned = {...gameStates}
          delete cleaned[0]
          gameStates = cleaned
        }
        // startUpdateAll optimistically marks every game 'syncing' before the
        // batch starts; with the batch never starting those states would keep
        // isUpdatingAny true and leave every button disabled. Reset active
        // phases back to idle so the rows and action buttons re-enable.
        const next = {...gameStates}
        let changed = false
        for (const [id, gs] of Object.entries(next)) {
          if (gs && UPDATE_BUSY_PHASES.includes(gs.phase)) {
            next[id] = {...gs, phase: 'idle'}
            changed = true
          }
        }
        if (changed) gameStates = next
        return
      }
      updateGS(data.gameID, {phase: 'error', error: data.message || 'Unknown error'})
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
      const next = {...gameStates}
      for (const [id, gs] of Object.entries(next)) {
        if (UPDATE_BUSY_PHASES.includes(gs.phase)) next[id] = {...gs, phase: 'error', error: 'Cancelled'}
      }
      gameStates = next
      if (batchState?.running) batchState = {...batchState, running: false, error: 'Cancelled'}
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
      try {
        await refreshGames()
        if (r?.total === 0) statusMsg = 'All games already have covers'
        else statusMsg = `Cover fetch complete — ${r?.fetched ?? 0} cached`
      } catch (e) {
        statusMsg = `Error: ${e}`
      }
    })
    unsubCoversError = EventsOn('covers:error', (r) => {
      coverState.coverError = r?.error || 'Cover fetch failed'
      coverState.fetching = false
    })
    return () => {
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
    {:else if activeView === 'detail' && selectedGameId !== null}
      <GameDetail gameId={selectedGameId} onBack={closeDetail} onUpdate={refreshGames}/>
    {:else if activeView === 'library'}
      <GameList {games} onOpenDetail={openDetail} onUpdate={refreshGames}/>
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
      <SettingsView />
    {:else if activeView === 'updates'}
      <GameUpdatesView
        gameStates={gameStates}
        batchState={batchState}
        onNavigate={(id) => activeView = id}
        onUpdateGame={startUpdateGame}
        onUpdateAll={startUpdateAll}
        onRetryFailed={handleRetryFailed}
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
      />
    {:else if activeView === 'browser'}
      <F95Browser />
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
          {#each deletedGames as game}
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

    <StatusBar {statusMsg} gameCount={games.length}/>
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
    min-width: 0;
    background: var(--bg-primary);
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
