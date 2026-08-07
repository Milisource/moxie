<script>
  import {onMount} from 'svelte'
  import {EventsOn} from '../wailsjs/runtime/runtime'
  import {GetGames, GetVersion, GetStartupError, ListDeletedGames, RestoreGame, PurgeDeleted} from '../wailsjs/go/main/App'
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
    games = await GetGames()
    statusMsg = `${games.length} game${games.length !== 1 ? 's' : ''} loaded`
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
    return () => {
      if (unsubAutoScan) unsubAutoScan()
      if (unsubAutoScanError) unsubAutoScanError()
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
      <ScanDialog onGamesUpdated={refreshGames}/>
    {:else if activeView === 'settings'}
      <SettingsView />
    {:else if activeView === 'updates'}
      <GameUpdatesView
        onNavigate={(id) => activeView = id}
        onUpdateCompleted={() => {
          refreshGames()
          lastUpdate++
        }}
      />
    {:else if activeView === 'downloads'}
      <DownloadsView />
    {:else if activeView === 'add'}
      <AddGameDialog onGameAdded={refreshGames}/>
    {:else if activeView === 'sync'}
      <SyncDialog />
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
