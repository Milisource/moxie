<script>
  import {initials} from './initials.js'
  import {coverRev} from './cover.js'
  import {onMount} from 'svelte'
  import {
    GetGameDetail, PlayGame, RemoveGame, SetGameStatus, RenameGame,
    SetGameWinePrefix, SyncSingleGame, EditGame,
    GetCollections, GetGameCollections, AddGameToCollection, RemoveGameFromCollection,
    GetInstallTargets, GetCoverBaseURL,
    OpenDownloadURL, OpenUpdateDownloadPage,
    FindCoverCandidates, SetGameCover, SetCoverLocked, RevertCover,
  } from '../../wailsjs/go/main/App'
  import {engineColor, engineOptions} from './engineColors.js'
  import {safeExternalUrl} from './sanitizeUrl.js'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {confirmAction} from './confirmDialog.svelte.js'
  import {openEditGame} from './editGameDialog.svelte.js'

  // The game-update and game-install pipelines (and their event subscriptions)
  // live in App.svelte so they survive tab switches. This view derives its
  // buttons from that shared state and routes actions back through the shell's
  // handlers — one source of truth for the backend's single-run lock (updates
  // and installs share it), instead of local flags that reset on remount while
  // the backend keeps running.

  let {
    gameId = null,
    onBack = () => {},
    onUpdate = () => {},
    gameState = null,            // gameStates[gameId] || null — shared pipeline state for this game
    installState = null,         // shared install pipeline state
    onUpdateGame = () => {},
    onProvideFile = () => {},
    onInstall = () => {},
  } = $props()

  const UPDATE_BUSY_PHASES = ['queued', 'syncing', 'selecting-link', 'downloading', 'extracting', 'merging', 'updating-db']

  let detail = $state(null)
  let loading = $state(true)
  let error = $state('')
  let showFullOverview = $state(false)
  let launchStatus = $state({msg: '', error: ''})

  // Visible error for inline edits / actions that fail — distinct from the
  // top-level `error` (which replaces the whole view) so a failed save shows
  // a notice while the editor stays open with the user's value.
  let editError = $state('')

  // Single-game F95Zone sync runs synchronously on the Wails call goroutine,
  // so the button owns its own busy state and the outcome is reported inline
  // next to the action buttons instead of the app-level status bar.
  let syncing = $state(false)
  let syncStatus = $state({msg: '', error: ''})

  // ── Game update (Update Available badge) ─────────────
  // Derived from the shared App-level gameStates entry: a busy phase means the
  // pipeline for THIS game is running. Other games' updates run in parallel
  // and don't lock this page.
  let updating = $derived(!!gameState && UPDATE_BUSY_PHASES.includes(gameState.phase))
  // Auto-download failed (Cloudflare-blocked host, …); the backend asked the
  // user to provide the archive manually via ProvideUpdateFile.
  let manualRequired = $derived(!!gameState?.manualRequired)
  let manualHost = $derived(gameState?.manualHost || '')
  let updateError = $derived(gameState?.phase === 'error' ? (gameState.error || 'Update failed') : '')
  let providingFile = $state(false)   // transient — the native file picker is modal

  // ── Install (browser-added games with no local copy) ──
  let installTargets = $state([])
  let installDest = $state('')
  let installBusy = $derived(!!installState?.running)
  let installForThis = $derived(installState?.gameId === Number(gameId))
  let installing = $derived(installForThis && installBusy)
  let installPhase = $derived(installForThis ? installState.phase : '')
  let installProgress = $derived(installForThis ? installState.progress : 0)
  let installError = $derived(installForThis ? installState.error : '')

  // The install UI tracks one install at a time (installState is shared), so
  // another game's install still locks the Install button.
  let installElsewhere = $derived(installBusy && !installing)

  // ── Download link rows ───────────────────────────────
  let openingLinks = $state(new Set())   // link IDs currently being opened

  function fmtErr(e) {
    return String(e).replace(/^Error:\s*/, '')
  }

  // ── Cover ────────────────────────────────────────
  // Local cached cover first (loopback HTTP, fast + offline), remote
  // F95Zone URL as a fallback when nothing is cached yet.
  let coverBase = $state('')
  let coverSrc = $state('')
  let coverFailed = $state(false)
  // Identifies the cover currently on screen (game id + remote URL) so a
  // failed cover stays failed across detail reloads, while a genuinely new
  // cover (different game or URL) gets a fresh attempt.
  let coverKey = $state('')

  function resolveCoverSrc(d) {
    if (!d) return ''
    // The local cached cover failed to load (e.g. cover server briefly down):
    // fall back to the remote F95Zone URL. The effect must NOT return ''
    // here or it would immediately wipe the fallback handleCoverError set.
    if (coverFailed) return d.coverUrl || ''
    // /large: long edge ≤ 1400px — the full original can be 3840px, a
    // ~60 MB decoded bitmap for one sidebar image.
    if (d.hasCover && coverBase) return `${coverBase}/cover/${d.id}/large${coverRev(d) ? '?v=' + coverRev(d) : ''}`
    if (d.coverUrl) return d.coverUrl
    return ''
  }

  function handleCoverError() {
    // The $effect re-runs on this flag and resolves the remote fallback.
    coverFailed = true
  }

  // ── Inline rename ───────────────────────────────
  let showRenameInput = $state(false)
  let renameTitle = $state('')

  // ── Inline field editing ────────────────────────
  let editExePath = $state({active: false, value: ''})
  let editVersion = $state({active: false, value: ''})
  let editNotes = $state({active: false, value: ''})
  let editWinePrefix = $state({active: false, value: ''})

  // ── Toggle sections ─────────────────────────────
  let showDownloads = $state(false)
  let showPlayHistory = $state(false)

  // ── Install (browser-added games with no local copy) ──
  // A /virtual/ path means the game exists only as an F95Zone reference.
  let needsInstall = $derived(!!detail?.path?.startsWith('/virtual/'))

  let installPhaseLabel = $derived.by(() => {
    switch (installPhase) {
      case 'selecting-link': return 'Finding link…'
      case 'downloading':    return 'Downloading…'
      case 'extracting':     return 'Extracting…'
      case 'installing':     return 'Installing…'
      case 'updating-db':    return 'Finalizing…'
      default:               return 'Working…'
    }
  })

  async function loadInstallTargets() {
    try {
      installTargets = (await GetInstallTargets()) || []
      const firstAvailable = installTargets.find(t => t.available)
      if (firstAvailable && !installDest) installDest = firstAvailable.path
    } catch (e) {
      installTargets = []
    }
  }

  // ── Collections ─────────────────────────────────
  let gameCollections = $state([])   // collections this game belongs to
  let allCollections = $state([])
  let collError = $state('')

  // Only offer collections the game is not already in.
  let availableCollections = $derived.by(() => {
    const member = new Set(gameCollections.map(c => c.id))
    return allCollections.filter(c => !member.has(c.id))
  })

  async function loadCollections() {
    if (!gameId) return
    try {
      const [mine, all] = await Promise.all([
        GetGameCollections(gameId),
        GetCollections(),
      ])
      gameCollections = mine || []
      allCollections = all || []
      collError = ''
    } catch (e) {
      collError = String(e)
    }
  }

  async function addToCollection(e) {
    const id = Number(e.target.value)
    e.target.value = ''
    if (!id) return
    try {
      await AddGameToCollection(gameId, id)
      await loadCollections()
      onUpdate()
    } catch (err) {
      collError = String(err)
    }
  }

  async function removeFromCollection(collectionId) {
    try {
      await RemoveGameFromCollection(gameId, collectionId)
      await loadCollections()
      onUpdate()
    } catch (err) {
      collError = String(err)
    }
  }

  async function loadDetail(silent = false) {
    if (!gameId) return
    // A sync refreshes the record in place; don't blank the whole detail view
    // for a frame just to re-show what the user is already looking at.
    if (!silent) loading = true
    error = ''
    try {
      detail = await GetGameDetail(gameId)
      // coverFailed is sticky per cover: only a different game (or a cover
      // URL that changed after a sync) gets a fresh chance to load. Resetting
      // here on every reload would re-thrash the failing cover on every
      // chatty game-update:* event (flicker between fallback and 'No Cover').
      const key = `${gameId}:${detail?.coverUrl || ''}`
      if (key !== coverKey) {
        coverKey = key
        coverFailed = false
      }
    } catch (e) {
      error = String(e)
    }
    loading = false
  }

  // ── Cover picker ─────────────────────────────────────
  // Candidates from Steam / SteamGridDB / VNDB (Settings → Cover art). Picking
  // one installs and locks it; Lock pins the current cover against syncs and
  // upgrades; Undo restores the cover the last pick/upgrade replaced.
  let picker = $state({open: false, loading: false, error: '', items: [], saving: ''})
  let coverMsg = $state('')
  async function openPicker() {
    picker = {open: true, loading: true, error: '', items: [], saving: ''}
    try {
      const items = await FindCoverCandidates(Number(gameId))
      picker = {...picker, loading: false, items: items || []}
    } catch (e) {
      picker = {...picker, loading: false, error: fmtErr(e)}
    }
  }
  async function pickCover(c) {
    picker = {...picker, saving: c.url, error: ''}
    try {
      await SetGameCover(Number(gameId), c.url, c.source)
      picker = {...picker, open: false, saving: ''}
      coverMsg = `Cover set from ${c.source} (locked)`
      await loadDetail()
      onUpdate()
    } catch (e) {
      picker = {...picker, saving: '', error: fmtErr(e)}
    }
  }
  async function toggleCoverLock() {
    try {
      await SetCoverLocked(Number(gameId), !detail.coverLocked)
      await loadDetail()
    } catch (e) {
      coverMsg = fmtErr(e)
    }
  }
  async function undoCover() {
    try {
      await RevertCover(Number(gameId))
      coverMsg = 'Previous cover restored (locked)'
      await loadDetail()
      onUpdate()
    } catch (e) {
      coverMsg = fmtErr(e)
    }
  }

  // Recompute the cover source whenever the detail, the cover server base,
  // or the error state changes.
  $effect(() => {
    coverSrc = resolveCoverSrc(detail)
  })

  // ── Engine colors — imported from shared module ───────────────
  // See engineColors.js for the canonical palette matching TUI styles

  function formatDuration(s) {
    if (!s) return ''
    const m = Math.floor(s / 60)
    const sec = s % 60
    if (m === 0) return `${sec}s`
    return `${m}m ${sec}s`
  }

  // ── Engine options for dropdown ─────────────
  // Derived from the canonical engineColors.js map so every engine the
  // detector can store (Unity, RenPy, Java, QSP, Tads, ADRIFT, WebGL,
  // Others, …) plus the manual-entry aliases appears in the list. If the
  // game carries a custom value not in the map (typed in manually), keep it
  // visible at the top rather than letting the browser fall back to option 1.
  let engineChoices = $derived.by(() => {
    const base = engineOptions()
    if (detail?.engine && !base.includes(detail.engine)) {
      return [detail.engine, ...base]
    }
    return base
  })

  // ── Action handlers ─────────────────────────────
  async function handleStatusChange(e) {
    const newStatus = e.target.value
    if (!gameId || newStatus === detail.status) return
    editError = ''
    try {
      await SetGameStatus(gameId, newStatus)
      await loadDetail()
      onUpdate()
    } catch (err) {
      editError = `Failed to update status: ${fmtErr(err)}`
    }
  }

  async function handleEngineChange(e) {
    const newEngine = e.target.value
    if (!gameId || newEngine === detail.engine) return
    editError = ''
    try {
      await EditGame(gameId, {engine: newEngine})
      await loadDetail()
      onUpdate()
    } catch (err) {
      editError = `Failed to update engine: ${fmtErr(err)}`
    }
  }

  function handleRenameStart() {
    renameTitle = detail.title
    showRenameInput = true
  }

  // Opens the shared Edit Game dialog (all fields at once). Refresh the detail
  // and the library on save so both reflect the change.
  async function handleEdit() {
    if (!gameId) return
    const saved = await openEditGame(gameId)
    if (saved) {
      await loadDetail()
      onUpdate()
    }
  }

  async function handleRenameSave() {
    const trimmed = renameTitle.trim()
    if (!trimmed || trimmed === detail.title) {
      showRenameInput = false
      return
    }
    editError = ''
    try {
      await RenameGame(gameId, trimmed)
      await loadDetail()
      onUpdate()
      // Only close the editor once the backend actually saved.
      showRenameInput = false
    } catch (err) {
      editError = `Failed to rename: ${fmtErr(err)}`
      // Keep the input open with the user's value so they can retry.
    }
  }

  function startExePathEdit() {
    editExePath = {active: true, value: detail.exePath || ''}
  }

  async function saveExePath() {
    editError = ''
    try {
      await EditGame(gameId, {exePath: editExePath.value})
      await loadDetail()
      onUpdate()
      editExePath = {active: false, value: ''}
    } catch (err) {
      editError = `Failed to save executable path: ${fmtErr(err)}`
      // Keep the editor open with the user's value so they can retry.
    }
  }

  function startWinePrefixEdit() {
    editWinePrefix = {active: true, value: detail.winePrefix || ''}
  }

  async function saveWinePrefix() {
    editError = ''
    try {
      await SetGameWinePrefix(gameId, editWinePrefix.value)
      await loadDetail()
      onUpdate()
      editWinePrefix = {active: false, value: ''}
    } catch (err) {
      editError = `Failed to save wine prefix: ${fmtErr(err)}`
      // Keep the editor open with the user's value so they can retry.
    }
  }

  function startVersionEdit() {
    editVersion = {active: true, value: detail.version || ''}
  }

  // Keyboard activation for the inline-edit spans: Enter or Space on the
  // focused element invokes the same action as a click (a11y).
  function onEditableKey(e, action) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      action()
    }
  }

  // "Mark as current": the installed version is unknown but the user knows
  // they already have the latest build. Same write path as the manual edit.
  // Fallback when the automatic download fails: open the deepest known
  // download page (resolved host page → masked link → thread).
  let openedPage = $state('')
  async function openDownloadPage() {
    try {
      openedPage = await OpenUpdateDownloadPage(gameId)
    } catch (e) {
      openedPage = `Could not open: ${e}`
    }
  }

  async function markAsCurrent() {
    editError = ''
    try {
      await EditGame(gameId, {version: detail.latestVersion})
      await loadDetail()
      onUpdate()
    } catch (err) {
      editError = `Failed to save version: ${fmtErr(err)}`
    }
  }

  async function saveVersion() {
    editError = ''
    try {
      await EditGame(gameId, {version: editVersion.value})
      await loadDetail()
      onUpdate()
      editVersion = {active: false, value: ''}
    } catch (err) {
      editError = `Failed to save version: ${fmtErr(err)}`
      // Keep the editor open with the user's value so they can retry.
    }
  }

  function startNotesEdit() {
    editNotes = {active: true, value: detail.notes || ''}
  }

  async function saveNotes() {
    editError = ''
    try {
      await EditGame(gameId, {notes: editNotes.value})
      await loadDetail()
      onUpdate()
      editNotes = {active: false, value: ''}
    } catch (err) {
      editError = `Failed to save notes: ${fmtErr(err)}`
      // Keep the editor open with the user's value so they can retry.
    }
  }

  async function handleRemove() {
    const ok = await confirmAction({
      title: 'Remove game?',
      description: `Are you sure you want to remove "${detail.title}" from your library?`,
      confirmLabel: 'Remove',
      danger: true,
    })
    if (!ok) return
    editError = ''
    try {
      await RemoveGame(gameId, false)
      onBack()
      onUpdate()
    } catch (err) {
      editError = `Failed to remove game: ${fmtErr(err)}`
      // Stay on the detail view — the game is still in the library.
    }
  }

  async function handleSync() {
    if (syncing) return
    editError = ''
    syncStatus = {msg: '', error: ''}
    // Capture what the user currently sees so the completion notice can name
    // the fields the scrape actually changed.
    const beforeLatest = detail?.latestVersion || ''
    const beforeVersion = detail?.version || ''
    syncing = true
    try {
      await SyncSingleGame(gameId)
      await loadDetail(true)
      onUpdate()
      syncStatus = {msg: syncSummary(beforeLatest, beforeVersion), error: ''}
    } catch (err) {
      syncStatus = {msg: '', error: `Failed to sync: ${fmtErr(err)}`}
    } finally {
      syncing = false
    }
  }

  // Wording for the post-sync notice. SyncSingleGame preserves a curated local
  // title and only refreshes F95Zone-sourced fields, so report the concrete
  // changes rather than a generic "done" that looks like nothing happened.
  function syncSummary(beforeLatest, beforeVersion) {
    const parts = []
    const latest = detail?.latestVersion || ''
    const version = detail?.version || ''
    if (latest && latest !== beforeLatest) parts.push(`latest version v${latest}`)
    if (version && version !== beforeVersion) parts.push(`installed version v${version}`)
    const base = parts.length
      ? `Synced from F95Zone — ${parts.join(', ')}`
      : 'Synced from F95Zone — metadata is up to date'
    return detail?.updateState === 'available' ? `${base}. Update available.` : base
  }

  async function handlePlay() {
    launchStatus = {msg: '', error: ''}
    try {
      const msg = await PlayGame(gameId)
      launchStatus = {msg, error: ''}
      await loadDetail()
      onUpdate()
    } catch (err) {
      launchStatus = {msg: '', error: String(err).replace(/^Error:\s*/, '')}
    }
  }

  // Open a download link's URL in the system browser (not the webview).
  async function handleOpenLink(linkId) {
    if (openingLinks.has(linkId)) return
    openingLinks = new Set([...openingLinks, linkId])
    try {
      await OpenDownloadURL(linkId)
    } catch (err) {
      editError = `Failed to open link: ${fmtErr(err)}`
    } finally {
      const next = new Set(openingLinks)
      next.delete(linkId)
      openingLinks = next
    }
  }

  onMount(async () => {
    try {
      coverBase = await GetCoverBaseURL()
    } catch (e) {
      console.error('Failed to get cover base URL', e)
    }
    loadDetail()
    loadCollections()
    loadInstallTargets()
  })

  // The App shell owns the game-update:* / game-install:* subscriptions and the
  // shared gameStates/installState. When a pipeline for THIS game lands in a
  // terminal phase we just re-fetch the detail and nudge the library — no
  // local event copies that can die on navigation.
  let prevUpdatePhase = ''
  $effect(() => {
    const phase = gameState?.phase || 'idle'
    if (phase !== prevUpdatePhase) {
      if (prevUpdatePhase !== '' && (phase === 'done' || phase === 'error')) {
        loadDetail()
        onUpdate()
      }
      prevUpdatePhase = phase
    }
  })

  let prevInstallPhase = ''
  $effect(() => {
    const phase = installForThis ? installState.phase : ''
    if (phase !== prevInstallPhase) {
      if (prevInstallPhase !== '' && (phase === 'done' || phase === 'error')) {
        loadDetail()
        onUpdate()
      }
      prevInstallPhase = phase
    }
  })
</script>

<div class="detail">
  <!-- Back Button -->
  <div class="detail-header">
    <button class="back-btn" onclick={onBack}>← Back to Library</button>
  </div>

  {#if loading}
    <div class="loading-state"><div class="spinner"></div><p>Loading game details…</p></div>
  {:else if error}
    <div class="error-state"><p>Error: {error}</p><button class="back-btn" onclick={onBack}>Go Back</button></div>
  {:else if detail}
    <div class="detail-content">
      <!-- Catalog layout: cover + actions | record | links & history.
           Columns collapse by width (see .detail-content media rules). -->
      <aside class="detail-side">
        <!-- Left: Cover -->
        <div class="cover-section">
          {#if coverSrc}
            <img
              class="cover-img"
              src={coverSrc}
              alt="{detail.title} cover"
              loading="lazy"
              decoding="async"
              onerror={handleCoverError}
            />
          {:else}
            <div class="cover-placeholder">
              <span class="cover-ph-initials">{initials(detail.title)}</span>
              <span class="cover-ph-note">No cover</span>
            </div>
          {/if}
        </div>
        <div class="cover-tools">
          <button class="cover-tool" onclick={openPicker} disabled={picker.loading}>
            {picker.loading ? 'Searching…' : 'Choose cover…'}
          </button>
          {#if detail.hasCover}
            <button class="cover-tool" class:cover-tool-on={detail.coverLocked} onclick={toggleCoverLock}
              title={detail.coverLocked ? 'Locked — syncs and upgrades leave this cover alone' : 'Lock this cover against syncs and upgrades'}>
              {detail.coverLocked ? 'Locked' : 'Lock'}
            </button>
          {/if}
          {#if detail.coverSource && detail.coverSource !== 'f95'}
            <button class="cover-tool" onclick={undoCover} title="Restore the cover this one replaced">Undo</button>
          {/if}
          {#if detail.coverW}
            <span class="cover-dims" title="Source: {detail.coverSource || 'f95'}">{detail.coverW}×{detail.coverH} · {detail.coverSource || 'f95'}</span>
          {/if}
        </div>
        {#if coverMsg}<p class="cover-msg">{coverMsg}</p>{/if}
        {#if picker.open}
          <div class="cover-picker">
            <div class="cover-picker-head">
              <span>Cover candidates</span>
              <button class="cover-tool" onclick={() => picker = {...picker, open: false}}>Close</button>
            </div>
            {#if picker.loading}
              <p class="cover-msg">Searching…</p>
            {:else if picker.error}
              <p class="cover-msg cover-msg-err">{picker.error}</p>
            {:else if picker.items.length === 0}
              <p class="cover-msg">No exact title match on the enabled sources (Settings → Cover art).</p>
            {/if}
            <div class="cover-picker-grid">
              {#each picker.items as c (c.url)}
                <button class="cand" onclick={() => pickCover(c)} disabled={!!picker.saving} title={c.note || c.source}>
                  <img src={c.thumb || c.url} alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer" />
                  <span class="cand-meta">{c.source} · {c.w}×{c.h}{c.h <= c.w ? ' · landscape' : ''}</span>
                  {#if picker.saving === c.url}<span class="cand-saving">Saving…</span>{/if}
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <!-- Action Buttons -->
        <div class="action-section">
          {#if launchStatus.msg}
            <div class="launch-notice launch-ok">{launchStatus.msg}</div>
          {/if}
          {#if launchStatus.error}
            <div class="launch-notice launch-error">{launchStatus.error}</div>
          {/if}
          {#if syncStatus.msg}
            <div class="launch-notice launch-ok">{syncStatus.msg}</div>
          {/if}
          {#if syncStatus.error}
            <div class="launch-notice launch-error">{syncStatus.error}</div>
          {/if}
          {#if needsInstall}
            <div class="install-box">
              <p class="install-hint">
                Added from F95Zone but not downloaded yet. Choose where to install it.
              </p>
              {#if installTargets.length === 0}
                <p class="install-warn">
                  No scan paths configured. Add one in Settings first.
                </p>
              {:else}
                <div class="install-row">
                  <select class="install-select" bind:value={installDest} disabled={installing || installElsewhere}>
                    {#each installTargets as t}
                      <option value={t.path} disabled={!t.available}>
                        {t.path}{t.available ? '' : ' (unavailable)'}
                      </option>
                    {/each}
                  </select>
                  <button
                    class="btn btn-primary"
                    onclick={() => onInstall(gameId, installDest)}
                    disabled={installing || !installDest || installElsewhere}
                    title={installElsewhere ? 'Another install is already running' : undefined}
                  >
                    {installing ? installPhaseLabel : installElsewhere ? 'Busy…' : '↓ Install'}
                  </button>
                </div>
                {#if installing && installProgress}
                  <div class="install-progress">
                    <div class="install-bar-bg">
                      <div class="install-bar-fill" style="width: {installProgress}%"></div>
                    </div>
                  </div>
                {/if}
              {/if}
              {#if installError}
                <p class="install-warn">{installError}</p>
              {/if}
            </div>
          {:else}
            <button class="btn btn-primary btn-play" onclick={handlePlay}>▶︎ Play</button>
          {/if}
          {#if detail.f95Url}
            <button class="btn" onclick={handleSync} disabled={syncing}>
              {syncing ? 'Syncing…' : 'Sync from F95Zone'}
            </button>
          {/if}
          <button class="btn" onclick={handleEdit}>Edit</button>
          <button class="btn" onclick={handleRenameStart}>Rename</button>
          <button class="btn btn-danger" onclick={handleRemove}>Remove Game</button>
        </div>
      </aside>

      <div class="detail-main">
        {#if editError}
          <div class="edit-notice">
            <span class="edit-notice-icon">✕</span>
            <span>{editError}</span>
          </div>
        {/if}

        <!-- Title + Badges -->
        <div class="title-section">
          <div class="title-main">
            {#if showRenameInput}
              <!-- svelte-ignore a11y_autofocus -->
              <input
                class="rename-input"
                type="text"
                bind:value={renameTitle}
                onkeydown={(e) => {
                  if (e.key === 'Enter') handleRenameSave()
                  if (e.key === 'Escape') showRenameInput = false
                }}
                autofocus
              />
              <button class="btn btn-sm btn-primary" onclick={handleRenameSave}>Save</button>
              <button class="btn btn-sm" onclick={() => showRenameInput = false}>Cancel</button>
            {:else}
              <h1>{detail.title}</h1>
              {#if detail.updateState === 'available' || detail.updateState === 'unknown' || updateError || manualRequired}
                {#if detail.updateState === 'unknown'}
                  <span class="update-badge update-badge-unknown" title="Installed version unknown; latest is {detail.latestVersion}">Installed: unknown</span>
                {:else if detail.updateState === 'available'}
                  <span class="update-badge" title="Update: {detail.version} → {detail.latestVersion}">Update Available</span>
                {/if}
                <button
                  class="update-btn"
                  onclick={() => onUpdateGame(gameId)}
                  disabled={updating || installing}
                  title="Download {detail.latestVersion}"
                >
                  {updating ? (gameState?.phase === 'queued' ? 'Queued…' : 'Downloading…') : '↓ Download Update'}
                </button>
                {#if detail.updateState === 'unknown'}
                  <button
                    class="btn btn-sm"
                    onclick={markAsCurrent}
                    disabled={updating}
                    title="Record {detail.latestVersion} as the installed version without downloading"
                  >Mark as current ({detail.latestVersion})</button>
                {/if}
                {#if updateError}
                  <span class="update-error" title={updateError}>{updateError}</span>
                {/if}
                {#if (updateError || manualRequired) && !updating}
                  <button
                    class="btn btn-sm"
                    onclick={openDownloadPage}
                    title="Open the download page in your browser"
                  >Open download page ↗</button>
                  {#if openedPage}
                    <span class="opened-page" title={openedPage}>{openedPage}</span>
                  {/if}
                {/if}
                {#if manualRequired && !updating}
                  <div class="manual-fallback">
                    <span class="manual-fallback-text">
                      Automatic download failed{manualHost ? ` (${manualHost} is blocking it)` : ''}.
                      Download the update in your browser, then point moxie at the archive.
                    </span>
                    <button
                      class="btn btn-sm btn-primary"
                      onclick={() => onProvideFile(gameId)}
                      disabled={providingFile || updating || installing}
                    >
                      {providingFile ? 'Selecting…' : 'Choose Downloaded Archive…'}
                    </button>
                  </div>
                {/if}
              {/if}
            {/if}
          </div>
          <div class="badges">
            {#if detail.engine}
              <span class="engine-badge" style="--ec: {engineColor(detail.engine)}">{detail.engine}</span>
            {/if}
            <span class="status-badge status-{detail.status || 'unknown'}">{statusLabel(detail.status)}</span>
          </div>
        </div>

        <!-- Right: Metadata -->
        <div class="meta-section">
          {#if detail.developer}
            <div class="meta-row">
              <span class="meta-label">Developer</span>
              <span class="meta-value">{detail.developer}</span>
            </div>
          {/if}

          <div class="meta-row">
            <span class="meta-label">Engine</span>
            <span class="meta-value">
              <span class="select-arrow">
                <select class="field-select" value={detail.engine || ''} onchange={handleEngineChange}>
                  <option value="">— None —</option>
                  {#each engineChoices as eng}
                    <option value={eng}>{eng}</option>
                  {/each}
                </select>
              </span>
            </span>
          </div>

          {#if detail.version || detail.latestVersion}
            <div class="meta-row">
              <span class="meta-label">Version</span>
              <span class="meta-value">
                {#if editVersion.active}
                  <input
                    class="inline-edit-input"
                    type="text"
                    bind:value={editVersion.value}
                    onkeydown={(e) => { if (e.key === 'Enter') saveVersion(); if (e.key === 'Escape') editVersion = {active: false, value: ''}; }}
                  />
                  <button class="btn btn-xs btn-primary" onclick={saveVersion}>Save</button>
                  <button class="btn btn-xs" onclick={() => editVersion = {active: false, value: ''}}>Cancel</button>
                {:else}
                  <span
                    class="editable-field"
                    role="button"
                    tabindex="0"
                    onclick={startVersionEdit}
                    onkeydown={(e) => onEditableKey(e, startVersionEdit)}
                    title="Click to edit"
                  >
                    {detail.version || 'unknown'}
                    {#if detail.updateState === 'available' || detail.updateState === 'unknown'}
                      <span class="version-update">→ {detail.latestVersion}</span>
                    {:else if detail.latestVersion && detail.updateState === 'current'}
                      <span class="version-current" title="Latest on F95Zone: {detail.latestVersion}">(latest)</span>
                    {/if}
                  </span>
                {/if}
              </span>
            </div>
          {/if}

          {#if detail.status}
            <div class="meta-row">
              <span class="meta-label">Status</span>
              <span class="meta-value">
                <span class="select-arrow">
                  <select class="field-select" onchange={handleStatusChange}>
                    {#each GAME_STATUSES as s}
                      <option value={s} selected={detail.status === s}>{statusLabel(s)}</option>
                    {/each}
                  </select>
                </span>
              </span>
            </div>
          {/if}

          {#if detail.exePath !== undefined}
            <div class="meta-row">
              <span class="meta-label">Executable</span>
              <span class="meta-value">
                {#if editExePath.active}
                  <input
                    class="inline-edit-input"
                    type="text"
                    bind:value={editExePath.value}
                    placeholder="/path/to/game.exe"
                    onkeydown={(e) => { if (e.key === 'Enter') saveExePath(); if (e.key === 'Escape') editExePath = {active: false, value: ''}; }}
                  />
                  <button class="btn btn-xs btn-primary" onclick={saveExePath}>Save</button>
                  <button class="btn btn-xs" onclick={() => editExePath = {active: false, value: ''}}>Cancel</button>
                {:else}
                  <span
                    class="editable-field mono"
                    role="button"
                    tabindex="0"
                    onclick={startExePathEdit}
                    onkeydown={(e) => onEditableKey(e, startExePathEdit)}
                    title="Click to edit"
                  >
                    {detail.exePath || '—'}
                  </span>
                {/if}
              </span>
            </div>
          {/if}

          {#if detail.winePrefix !== undefined}
            <div class="meta-row">
              <span class="meta-label">Wine Prefix</span>
              <span class="meta-value">
                {#if editWinePrefix.active}
                  <input
                    class="inline-edit-input"
                    type="text"
                    bind:value={editWinePrefix.value}
                    placeholder="/path/to/wineprefix"
                    title="Leave empty to clear"
                    onkeydown={(e) => { if (e.key === 'Enter') saveWinePrefix(); if (e.key === 'Escape') editWinePrefix = {active: false, value: ''}; }}
                  />
                  <button class="btn btn-xs btn-primary" onclick={saveWinePrefix}>Save</button>
                  <button class="btn btn-xs" onclick={() => editWinePrefix = {active: false, value: ''}}>Cancel</button>
                {:else}
                  <span
                    class="editable-field mono"
                    role="button"
                    tabindex="0"
                    onclick={startWinePrefixEdit}
                    onkeydown={(e) => onEditableKey(e, startWinePrefixEdit)}
                    title="Click to edit"
                  >
                    {detail.winePrefix || '— (system default)'}
                  </span>
                {/if}
              </span>
            </div>
          {/if}

          {#if detail.sizeLabel}
            <div class="meta-row">
              <span class="meta-label">Size</span>
              <span class="meta-value">{detail.sizeLabel}</span>
            </div>
          {/if}

          {#if detail.path}
            <div class="meta-row">
              <span class="meta-label">Path</span>
              <span class="meta-value mono">{detail.path}</span>
            </div>
          {/if}

          {#if detail.steamAppId && detail.steamAppId > 0}
            <div class="meta-row">
              <span class="meta-label">Steam</span>
              <span class="meta-value">
                <a href="https://store.steampowered.com/app/{detail.steamAppId}" target="_blank" rel="noopener">
                  App {detail.steamAppId}
                </a>
              </span>
            </div>
          {/if}

          <div class="meta-row">
            <span class="meta-label">Collections</span>
            <span class="meta-value">
              <div class="coll-chips">
                {#each gameCollections as c}
                  <span class="coll-chip">
                    {c.name}
                    <button
                      class="coll-chip-x"
                      title="Remove from {c.name}"
                      onclick={() => removeFromCollection(c.id)}
                    >✕</button>
                  </span>
                {/each}
                {#if gameCollections.length === 0}
                  <span class="text-muted">None</span>
                {/if}
              </div>
              {#if availableCollections.length > 0}
                <select class="coll-select" value="" onchange={addToCollection}>
                  <option value="" disabled selected>Add to collection…</option>
                  {#each availableCollections as c}
                    <option value={c.id}>{c.name}</option>
                  {/each}
                </select>
              {/if}
              {#if collError}
                <span class="coll-error">{collError}</span>
              {/if}
            </span>
          </div>

          {#if detail.tags && detail.tags.length}
            <div class="meta-row">
              <span class="meta-label">Tags</span>
              <span class="meta-value tags">
                {#each detail.tags as tag}
                  <span class="tag">{tag}</span>
                {/each}
              </span>
            </div>
          {/if}

          {#if detail.f95Url && safeExternalUrl(detail.f95Url)}
            <div class="meta-row">
              <span class="meta-label">F95Zone</span>
              <span class="meta-value"><a href={safeExternalUrl(detail.f95Url)} target="_blank" rel="noopener">{detail.f95Url}</a></span>
            </div>
          {/if}

          {#if detail.storeLinks && Object.keys(detail.storeLinks).length > 0}
            <div class="meta-row">
              <span class="meta-label">Stores</span>
              <span class="meta-value store-links">
                {#each Object.entries(detail.storeLinks) as [store, url]}
                  {#if safeExternalUrl(url)}
                    <a href={safeExternalUrl(url)} target="_blank" rel="noopener" class="store-badge">{store}</a>
                  {/if}
                {/each}
              </span>
            </div>
          {/if}
        </div>

        <!-- Overview -->
        {#if detail.overview}
          <div class="overview-section">
            <h3>Overview</h3>
            <div class="overview-text" class:truncated={!showFullOverview && detail.overview.length > 500}>
              {showFullOverview ? detail.overview : detail.overview.slice(0, 500)}
              {#if detail.overview.length > 500}
                <button class="show-more" onclick={() => showFullOverview = !showFullOverview}>
                  {showFullOverview ? 'Show less' : '… Show more'}
                </button>
              {/if}
            </div>
          </div>
        {/if}

        <!-- Notes -->
        {#if detail.notes !== undefined || editNotes.active}
          <div class="section-card notes-section">
            <div class="section-card-header">
              <h3>Notes</h3>
              {#if !editNotes.active}
                <button class="btn btn-xs" onclick={startNotesEdit}>
                  {detail.notes ? 'Edit' : 'Add Note'}
                </button>
              {/if}
            </div>
            {#if editNotes.active}
              <textarea
                class="notes-textarea"
                bind:value={editNotes.value}
                placeholder="Add notes about this game..."
                rows="3"
              ></textarea>
              <div class="inline-edit-actions">
                <button class="btn btn-xs btn-primary" onclick={saveNotes}>Save</button>
                <button class="btn btn-xs" onclick={() => editNotes = {active: false, value: ''}}>Cancel</button>
              </div>
            {:else if detail.notes}
              <p class="notes-text">{detail.notes}</p>
            {:else}
              <p class="notes-empty">No notes.</p>
            {/if}
          </div>
        {/if}

      </div>

      <div class="detail-extra">
        <!-- Download Links -->
        {#if detail.downloadLinks && detail.downloadLinks.length > 0}
          <div class="section-card">
            <div class="section-card-header">
              <h3>Download Links ({detail.downloadLinks.length})</h3>
              <button class="btn btn-xs" onclick={() => showDownloads = !showDownloads}>
                {showDownloads ? 'Hide' : 'Show'}
              </button>
            </div>
            {#if showDownloads}
              <div class="dl-list">
                {#each detail.downloadLinks as link}
                  <button
                    class="dl-row"
                    onclick={() => handleOpenLink(link.id)}
                    disabled={openingLinks.has(link.id)}
                    title="Open download page in your browser"
                  >
                    <span class="dl-host">{link.host}</span>
                    <span class="dl-name">{link.name}</span>
                    <span class="dl-url">{safeExternalUrl(link.url) || link.url}</span>
                    {#if link.platform}
                      <span class="dl-platform">{link.platform}</span>
                    {/if}
                    {#if link.isDead}
                      <span class="dl-dead">dead</span>
                    {/if}
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        {/if}

        <!-- Play History -->
        {#if detail.playHistory && detail.playHistory.length > 0}
          <div class="section-card">
            <div class="section-card-header">
              <h3>Play History ({detail.playHistory.length})</h3>
              <button class="btn btn-xs" onclick={() => showPlayHistory = !showPlayHistory}>
                {showPlayHistory ? 'Hide' : 'Show'}
              </button>
            </div>
            {#if showPlayHistory}
              <div class="ph-list">
                {#each detail.playHistory as entry}
                  <div class="ph-row">
                    <span class="ph-date">{new Date(entry.playedAt).toLocaleDateString()}</span>
                    <span class="ph-time">{new Date(entry.playedAt).toLocaleTimeString()}</span>
                    {#if entry.platform}
                      <span class="ph-platform">{entry.platform}</span>
                    {/if}
                    {#if entry.durationS}
                      <span class="ph-duration">{formatDuration(entry.durationS)}</span>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/if}

      </div>
    </div>
  {/if}
</div>

<style>
  .detail {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: auto;
  }

  .detail-header {
    flex-shrink: 0;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-secondary);
  }

  .back-btn {
    padding: 4px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-base);
    cursor: pointer;
  }
  .back-btn:hover { background: var(--bg-hover); }

  /* Catalog record layout. Side column (cover + actions) scales with the
     window; the record fills the rest; at >=2200px links and history get
     their own third column instead of trailing below. */
  .detail-content {
    flex: 1;
    display: grid;
    grid-template-columns: clamp(220px, 24vw, 520px) minmax(0, 1fr);
    align-items: start;
    gap: var(--space-8) clamp(24px, 2.5vw, 56px);
    padding: var(--space-7) var(--gutter);
    width: 100%;
  }
  .detail-side {
    position: sticky;
    top: var(--space-7);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    min-width: 0;
  }
  .detail-main { min-width: 0; }
  .detail-extra { grid-column: 2; min-width: 0; }
  .detail-extra:empty { display: none; }
  .detail-extra > :first-child { margin-top: 0; }
  @media (min-width: 2200px) {
    .detail-content {
      grid-template-columns: clamp(220px, 20vw, 520px) minmax(0, 1fr) minmax(360px, 26vw);
    }
    .detail-extra { grid-column: 3; grid-row: 1; }
  }
  @media (max-width: 900px) {
    .detail-content { grid-template-columns: minmax(0, 1fr); }
    .detail-side { position: static; }
    .detail-extra { grid-column: 1; }
  }

  /* ── Title ──────────────────────────────── */
  .title-section {
    margin-bottom: 24px;
  }
  .title-main {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 8px;
  }
  .title-main {
    flex-wrap: wrap;
  }
  .title-main h1 {
    font-size: clamp(var(--text-2xl), 2.2vw, var(--text-4xl));
    font-weight: 600;
    line-height: 1.1;
    margin: 0;
    flex-basis: 100%;
  }
  .title-section {
    padding-bottom: var(--space-5);
    border-bottom: var(--rule-emph);
  }
  .update-badge {
    padding: 2px 10px;
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--warning) 20%, transparent);
    color: var(--warning);
    font-size: var(--text-sm);
    font-weight: 600;
  }
  .update-badge-unknown {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .update-btn {
    padding: 3px 12px;
    border: 1px solid var(--accent);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    color: var(--accent);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background 0.12s, color 0.12s;
  }
  .update-btn:hover:not(:disabled) { background: var(--accent); color: var(--on-accent); }
  .update-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .opened-page {
    display: block;
    max-width: 100%;
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .update-error {
    font-size: var(--text-xs);
    color: var(--danger);
    font-family: var(--font-mono);
    max-width: 420px;
  }
  .manual-fallback {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-top: 10px;
    padding: 10px 14px;
    border: 1px solid var(--warning);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--warning) 8%, transparent);
    max-width: 560px;
  }
  .manual-fallback-text {
    font-size: var(--text-sm);
    color: var(--text-secondary);
    line-height: 1.4;
    flex: 1;
    min-width: 220px;
  }
  .badges {
    display: flex;
    gap: 8px;
  }
  /* Square, bordered mono tags (REN'PY / ACTIVE), not pills. */
  .engine-badge,
  .status-badge {
    padding: 1px 7px;
    border: 1px solid currentColor;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .engine-badge { color: var(--ec); }
  .status-active { color: var(--success); }
  .status-completed { color: var(--accent); }
  .status-abandoned { color: var(--text-muted); }
  .status-on_hold { color: var(--warning); }
  .status-unknown { color: var(--text-muted); }

  /* ── Grid ───────────────────────────────── */
  .cover-section {
    display: flex;
  }
  .cover-tools { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 8px; }
  .cover-tool {
    padding: 3px 9px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--text-xs);
    cursor: pointer;
  }
  .cover-tool:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .cover-tool:disabled { opacity: 0.5; cursor: default; }
  .cover-tool-on { border-color: var(--accent); color: var(--accent); }
  .cover-dims { margin-left: auto; font-family: var(--font-mono); font-size: var(--text-xs); color: var(--text-muted); }
  .cover-msg { margin: 6px 0 0; font-size: var(--text-xs); color: var(--text-muted); }
  .cover-msg-err { color: var(--danger); }
  .cover-picker { margin-top: 10px; padding: 10px; border: 1px solid var(--border); background: var(--bg-secondary); }
  .cover-picker-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; font-size: var(--text-sm); color: var(--text-secondary); }
  .cover-picker-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 8px; }
  .cand {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 0;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    cursor: pointer;
    text-align: left;
  }
  .cand:hover:not(:disabled) { border-color: var(--accent); }
  .cand img { width: 100%; aspect-ratio: 3 / 4; object-fit: cover; display: block; }
  .cand-meta { padding: 0 6px 5px; font-size: var(--text-xs); font-family: var(--font-mono); color: var(--text-muted); }
  .cand-saving {
    position: absolute; inset: 0;
    display: flex; align-items: center; justify-content: center;
    background: rgba(0, 0, 0, 0.6); color: var(--text-primary); font-size: var(--text-sm);
  }
  .cover-img {
    display: block;
    /* Render at the image's own resolution — never upscale past the source,
       so low-res covers stay as sharp as the file allows and high-res ones
       get shown at full quality. Only oversized images are downscaled, and
       the natural aspect ratio is always preserved (no crop, no distortion). */
    width: auto;
    height: auto;
    max-width: 100%;
    max-height: 75vh;
    outline: 1px solid var(--border);
    outline-offset: -1px;
  }
  .cover-placeholder {
    width: 100%;
    aspect-ratio: 3/4;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    background:
      repeating-linear-gradient(to bottom, transparent 0 23px, var(--border) 23px 24px),
      var(--bg-tertiary);
    color: var(--text-muted);
  }
  .cover-ph-initials {
    font-family: var(--font-display);
    font-size: clamp(var(--text-4xl), 5vw, 96px);
    font-weight: 600;
    line-height: 1;
    color: var(--text-primary);
    opacity: 0.85;
  }
  .cover-ph-note {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
  }

  /* Tabular record: label column + value, hairline between rows. */
  .meta-section {
    display: flex;
    flex-direction: column;
    margin: var(--space-5) 0 var(--space-7);
  }
  .meta-row {
    display: grid;
    grid-template-columns: 140px minmax(0, 1fr);
    gap: var(--space-4);
    align-items: baseline;
    padding: 7px 0;
    border-bottom: var(--rule);
  }
  .meta-label {
    font-family: var(--font-display);
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
  }
  .meta-value {
    font-size: var(--text-md);
    color: var(--text-primary);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .meta-value a { color: var(--accent); text-decoration: none; }
  .meta-value a:hover { text-decoration: underline; }

  .version-update {
    color: var(--warning);
    font-weight: 600;
  }
  .version-current {
    color: var(--text-muted);
    font-size: var(--text-sm);
  }

  .tags { display: flex; flex-wrap: wrap; gap: 4px; }
  .tag {
    padding: 1px 6px;
    border-radius: var(--radius-1);
    background: var(--bg-tertiary);
    font-size: var(--text-xs);
    color: var(--text-secondary);
  }

  /* ── Install ────────────────────────────── */
  .install-box {
    width: 100%;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    margin-bottom: 8px;
  }
  .install-hint { margin: 0 0 8px; font-size: var(--text-sm); color: var(--text-secondary); }
  .install-warn { margin: 8px 0 0; font-size: var(--text-sm); color: var(--warning); }
  .install-row { display: flex; gap: 8px; align-items: center; }
  .install-select {
    flex: 1;
    padding: 6px 26px 6px 10px;
    font-size: var(--text-sm);
    font-family: var(--font-mono);
    background-color: var(--bg-tertiary);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
  }
  .install-progress { margin-top: 8px; }
  .install-bar-bg {
    height: 5px;
    border-radius: var(--radius-1);
    background: var(--bg-tertiary);
    overflow: hidden;
  }
  .install-bar-fill {
    height: 100%;
    border-radius: var(--radius-1);
    background: var(--accent);
    transition: width 0.2s ease;
  }

  /* ── Collections ────────────────────────── */
  .coll-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    align-items: center;
  }
  .coll-chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 4px 2px 8px;
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    font-size: var(--text-xs);
    font-weight: 600;
  }
  .coll-chip-x {
    border: none;
    background: transparent;
    color: inherit;
    cursor: pointer;
    font-size: var(--text-2xs);
    line-height: 1;
    padding: 2px 3px;
    border-radius: 50%;
    opacity: 0.7;
  }
  .coll-chip-x:hover { opacity: 1; background: color-mix(in srgb, var(--danger) 25%, transparent); }

  .coll-select {
    margin-top: 6px;
    padding: 3px 26px 3px 8px;
    font-size: var(--text-sm);
    background-color: var(--bg-tertiary);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    cursor: pointer;
  }
  .coll-error {
    display: block;
    margin-top: 4px;
    font-size: var(--text-xs);
    color: var(--danger);
  }

  /* ── Overview ───────────────────────────── */
  .overview-section {
    padding-top: var(--space-3);
  }
  .overview-section h3,
  .section-card h3 {
    font-family: var(--font-display);
    font-size: var(--text-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-secondary);
    margin: 0 0 var(--space-3);
  }
  .overview-text {
    max-width: 78ch;
    font-size: var(--text-md);
    line-height: 1.65;
    color: var(--text-primary);
    white-space: pre-wrap;
  }
  .overview-text.truncated {
    max-height: 200px;
    overflow: hidden;
    position: relative;
  }
  .show-more {
    display: block;
    margin-top: 8px;
    background: none;
    border: none;
    color: var(--accent);
    font-size: var(--text-base);
    cursor: pointer;
    padding: 0;
  }
  .show-more:hover { color: var(--accent-hover); }

  /* ── States ─────────────────────────────── */
  .loading-state, .error-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    flex: 1;
    gap: 12px;
    color: var(--text-muted);
  }
  .spinner {
    width: 24px; height: 24px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Inline Rename ─────────────────────────── */
  .rename-input {
    flex: 1;
    padding: 6px 10px;
    border: 1px solid var(--accent);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-2xl);
    font-weight: 700;
    outline: none;
    min-width: 0;
  }

  /* ── Buttons ───────────────────────────────── */
  .btn {
    padding: 7px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-base);
    cursor: pointer;
    transition: background 0.12s;
  }
  .btn:hover { background: var(--bg-hover); }
  .btn-sm {
    padding: 4px 10px;
    font-size: var(--text-sm);
  }
  .btn-primary {
    background: var(--accent);
    color: var(--on-accent);
    border-color: var(--accent);
  }
  .btn-primary:hover { background: var(--accent-hover); }
  .btn-danger {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 30%, transparent);
  }
  .btn-danger:hover { background: color-mix(in srgb, var(--danger) 10%, transparent); }

  /* ── Field Select (Engine, Status) ─────────── */
  .select-arrow {
    position: relative;
    display: inline-block;
  }
  .select-arrow::after {
    content: '';
    position: absolute;
    right: 8px;
    top: 50%;
    transform: translateY(-50%);
    width: 0;
    height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid var(--text-muted);
    pointer-events: none;
  }
  .field-select {
    padding: 2px 22px 2px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background-color: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-base);
    outline: none;
    cursor: pointer;
    max-width: 180px;
    /* Strip native OS widget — CSS controls appearance fully */
    -webkit-appearance: none;
    -moz-appearance: none;
    appearance: none;
  }
  .field-select:focus { border-color: var(--accent); }
  .field-select:hover { border-color: var(--accent-hover); }
  .field-select option {
    background: var(--bg-primary);
    color: var(--text-primary);
  }

  /* ── Inline Edit Input ─────────────────────── */
  .inline-edit-input {
    padding: 3px 6px;
    border: 1px solid var(--accent);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-base);
    outline: none;
    width: 180px;
  }
  .inline-edit-actions {
    display: flex;
    gap: 4px;
    margin-top: 6px;
  }

  /* ── Editable Field ────────────────────────── */
  .editable-field {
    cursor: pointer;
    border-bottom: 1px dashed var(--text-muted);
    padding-bottom: 1px;
  }
  .editable-field:hover {
    border-bottom-color: var(--accent);
    color: var(--accent);
  }

  /* ── Button xs ─────────────────────────────── */
  .btn-xs {
    padding: 2px 8px;
    font-size: var(--text-xs);
    border-radius: var(--radius-1);
  }

  /* ── Mono text ─────────────────────────────── */
  .mono {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
  }

  /* ── Store Links ───────────────────────────── */
  .store-links { display: flex; flex-wrap: wrap; gap: 4px; }
  .store-badge {
    padding: 1px 7px;
    border-radius: var(--radius-1);
    font-size: var(--text-xs);
    font-weight: 600;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
    text-decoration: none;
    text-transform: capitalize;
  }
  .store-badge:hover { background: var(--accent); color: var(--on-accent); }

  /* ── Section Cards ─────────────────────────── */
  .section-card {
    border-top: var(--rule-emph);
    padding: var(--space-4) 0 0;
    margin-top: var(--space-7);
  }
  .section-card-header h3 { margin: 0; }
  .section-card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  /* ── Notes ─────────────────────────────────── */
  .notes-text {
    font-size: var(--text-base);
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    margin: 0;
  }
  .notes-empty {
    font-size: var(--text-base);
    color: var(--text-muted);
    margin: 0;
    font-style: italic;
  }
  .notes-textarea {
    width: 100%;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-base);
    font-family: inherit;
    outline: none;
    resize: vertical;
    box-sizing: border-box;
  }
  .notes-textarea:focus { border-color: var(--accent); }

  /* ── Download Links ────────────────────────── */
  .dl-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 240px;
    overflow-y: auto;
  }
  .dl-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    font-size: var(--text-sm);
    border-radius: var(--radius-1);
    /* Button reset — the whole row is the open action */
    background: transparent;
    border: none;
    width: 100%;
    text-align: left;
    color: inherit;
    font-family: inherit;
    cursor: pointer;
  }
  .dl-row:hover:not(:disabled) { background: var(--bg-hover); }
  .dl-row:disabled { opacity: 0.6; cursor: default; }
  .dl-host {
    font-weight: 600;
    color: var(--text-primary);
    min-width: 70px;
    flex-shrink: 0;
  }
  .dl-name {
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 160px;
    flex-shrink: 1;
  }
  .dl-url {
    flex: 1;
    min-width: 0;
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dl-platform {
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
    flex-shrink: 0;
  }
  .dl-dead {
    font-size: var(--text-2xs);
    color: var(--danger);
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  /* ── Play History ──────────────────────────── */
  .ph-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 240px;
    overflow-y: auto;
  }
  .ph-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    font-size: var(--text-sm);
    border-radius: var(--radius-1);
  }
  .ph-row:hover { background: var(--bg-hover); }
  .ph-date {
    min-width: 80px;
    color: var(--text-primary);
  }
  .ph-time {
    color: var(--text-muted);
    min-width: 60px;
  }
  .ph-platform {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
  }
  .ph-duration {
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    margin-left: auto;
  }

  /* ── Action Section ────────────────────────── */
  /* Actions stack under the cover; Play is the full-width primary. */
  .action-section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    align-items: stretch;
  }
  .action-section > .btn { width: 100%; justify-content: center; }

  .btn-play {
    padding-block: 10px;
    font-family: var(--font-display);
    font-size: var(--text-md);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
  }

  .launch-notice {
    width: 100%;
    padding: 8px 12px;
    border-radius: var(--radius-1);
    font-size: var(--text-base);
  }
  .launch-ok {
    color: var(--success);
    background: color-mix(in srgb, var(--success) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--success) 35%, transparent);
  }
  .launch-error {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
  }

  /* ── Inline Edit / Action Error Notice ─────── */
  .edit-notice {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    margin-bottom: 16px;
    border-radius: var(--radius-1);
    font-size: var(--text-base);
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
  }
  .edit-notice-icon {
    font-weight: 700;
    flex-shrink: 0;
    line-height: 1.4;
  }
</style>
