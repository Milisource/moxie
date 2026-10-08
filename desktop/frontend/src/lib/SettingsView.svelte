<script>
  import {onMount} from 'svelte'
  import {
    CheckDependencies,
    GetDbPath,
    GetConfigDir,
    GetVersion,
    GetUpdateConcurrency,
    SetUpdateConcurrency,
    GetOrganizeInstalls,
    SetOrganizeInstalls,
    GetCoverArtSettings,
    SetCoverSources,
    SetSteamGridDBKey,
  } from '../../wailsjs/go/main/App'
  import ScanPaths from './ScanPaths.svelte'
  import UpdateDialog from './UpdateDialog.svelte'

  let {
    appVersion = '',
    appUpdateState = {
      checking: false,
      info: null,
      downloading: false,
      downloadProgress: {downloaded: 0, total: 0},
      downloadComplete: false,
      error: '',
    },
    onCheckAppUpdate = () => {},
    onDownloadAppUpdate = () => {},
    onApplyAppUpdate = () => {},
  } = $props()

  let deps = $state([])
  let dbPath = $state('')
  let configDir = $state('')
  let version = $state('')
  let checking = $state(true)
  let error = $state('')

  async function loadAll() {
    checking = true
    try {
      const [d, db, cfg, v] = await Promise.all([
        CheckDependencies(),
        GetDbPath(),
        GetConfigDir(),
        GetVersion(),
      ])
      deps = d || []
      dbPath = db
      configDir = cfg
      version = v
      error = ''
    } catch (e) {
      error = `Failed to load settings: ${e}`
    }
    checking = false
  }

  onMount(loadAll)

  // Parallel game updates (config key update-concurrency, 1–4). Applies to
  // queued runs immediately — no restart.
  let concurrency = $state(2)
  let concurrencyError = $state('')
  onMount(async () => {
    try { concurrency = await GetUpdateConcurrency() } catch (e) { /* keep default */ }
  })
  // Cover art sources (Steam keyless, SteamGridDB key, VNDB opt-in). The
  // key is write-only from here: the backend only reports its last 4 chars.
  let coverArt = $state({steam: true, vndb: false, sgdbKeySet: false, sgdbKeyHint: '', sgdbFromEnv: false})
  let sgdbInput = $state('')
  let coverArtMsg = $state('')
  async function loadCoverArt() {
    try { coverArt = await GetCoverArtSettings() } catch (e) { /* keep defaults */ }
  }
  onMount(loadCoverArt)
  async function toggleSource(key) {
    coverArtMsg = ''
    const next = {...coverArt, [key]: !coverArt[key]}
    try {
      await SetCoverSources(next.steam, next.vndb)
      coverArt = next
    } catch (e) {
      coverArtMsg = `Could not save: ${e}`
    }
  }
  async function saveSGDBKey(clear = false) {
    coverArtMsg = ''
    try {
      await SetSteamGridDBKey(clear ? '' : sgdbInput)
      sgdbInput = ''
      await loadCoverArt()
      coverArtMsg = clear ? 'Key removed.' : 'Key saved.'
    } catch (e) {
      coverArtMsg = `Could not save: ${e}`
    }
  }

  async function saveConcurrency(n) {
    concurrencyError = ''
    try {
      await SetUpdateConcurrency(n)
      concurrency = n
    } catch (e) {
      concurrencyError = `Could not save: ${e}`
    }
  }

  // Organize fresh installs by engine folder (config key
  // organize-installs-by-engine): when on, a game installs to
  // <scan path>/<ENGINE>/<Title> instead of directly under the scan path.
  let organizeInstalls = $state(false)
  let organizeError = $state('')
  onMount(async () => {
    try { organizeInstalls = await GetOrganizeInstalls() } catch (e) { /* keep default */ }
  })
  async function toggleOrganizeInstalls() {
    organizeError = ''
    const next = !organizeInstalls
    try {
      await SetOrganizeInstalls(next)
      organizeInstalls = next
    } catch (e) {
      organizeError = `Could not save: ${e}`
    }
  }

  function statusLabel(s) {
    switch (s) {
      case 'ok': return 'OK'
      case 'not_found': return 'Not found'
      default: return s
    }
  }
</script>

<div class="settings-view">
  <div class="settings-header">
    <h2>Settings</h2>
    <p class="settings-subtitle">Scan locations, system dependencies, and where Moxie keeps its data.</p>
  </div>

  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <!-- ── Scan Paths ─────────────────────────────────────── -->
  <section class="settings-section">
    <h3 class="section-title">Scan Paths</h3>
    <p class="section-hint">
      Directories watched for new and removed games. Changes take effect immediately.
    </p>
    <ScanPaths />
  </section>

  <!-- ── Install Location ───────────────────────────────── -->
  <section class="settings-section">
    <h3 class="section-title">Install Location</h3>
    <p class="section-hint">
      Where games downloaded from the F95Zone Browser are installed. Scan paths
      are the available destinations; this controls the subfolder.
    </p>
    <label class="kv-row toggle-row">
      <span class="kv-key">Organize installs by engine <span class="muted">— place each game in an engine subfolder</span></span>
      <input type="checkbox" checked={organizeInstalls} onchange={toggleOrganizeInstalls} />
    </label>
    <p class="section-hint">
      On: a game installs to <code>&lt;scan path&gt;/HTML/</code>,
      <code>…/RPGM/</code>, <code>…/JRE/</code> and so on. Off: directly under
      the scan path. Existing installs are not moved.
    </p>
    {#if organizeError}<p class="section-hint error-text">{organizeError}</p>{/if}
  </section>

  <!-- ── Dependencies ───────────────────────────────────── -->
  <section class="settings-section">
    <div class="section-head">
      <h3 class="section-title">Dependencies</h3>
      <button class="btn btn-outline" onclick={loadAll} disabled={checking}>
        {checking ? 'Checking…' : 'Re-check'}
      </button>
    </div>
    {#if deps.length === 0 && !checking}
      <p class="section-hint">No dependency information available.</p>
    {:else}
      <div class="dep-list">
        {#each deps as dep}
          <div class="dep-row">
            <span class="dep-dot" class:dep-ok={dep.status === 'ok'}></span>
            <span class="dep-name">{dep.name}</span>
            <span class="dep-status" class:status-ok={dep.status === 'ok'}>
              {statusLabel(dep.status)}
            </span>
            <span class="dep-details" title={dep.details}>{dep.details}</span>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <!-- ── Game Updates ───────────────────────────────────── -->
  <section class="settings-section">
    <h3 class="section-title">Game Updates</h3>
    <p class="section-hint">
      How many game updates download and install at once. Extra updates wait
      in a queue. Link unwrapping and browser fallbacks stay one-at-a-time, so
      F95Zone is never hit harder.
    </p>
    <div class="kv-row">
      <span class="kv-key">Parallel updates</span>
      <div class="seg" role="radiogroup" aria-label="Parallel updates">
        {#each [1, 2, 3, 4] as n}
          <button
            class="seg-btn"
            class:seg-active={concurrency === n}
            role="radio"
            aria-checked={concurrency === n}
            onclick={() => saveConcurrency(n)}
          >{n}</button>
        {/each}
      </div>
    </div>
    {#if concurrencyError}<p class="section-hint error-text">{concurrencyError}</p>{/if}
  </section>

  <!-- ── Cover Art ──────────────────────────────────────── -->
  <section class="settings-section">
    <h3 class="section-title">Cover Art</h3>
    <p class="section-hint">
      Where Covers → Upgrade to Portrait Art and a game's "Choose cover…" look
      for box art. Matches need an exact title.
    </p>
    <div class="kv-list">
      <label class="kv-row toggle-row">
        <span class="kv-key">Steam <span class="muted">— library capsules, no key</span></span>
        <input type="checkbox" checked={coverArt.steam} onchange={() => toggleSource('steam')} />
      </label>
      <label class="kv-row toggle-row">
        <span class="kv-key">VNDB <span class="muted">— visual novel covers, often small</span></span>
        <input type="checkbox" checked={coverArt.vndb} onchange={() => toggleSource('vndb')} />
      </label>
      <div class="kv-row">
        <span class="kv-key">SteamGridDB key</span>
        <span class="kv-value">
          {#if coverArt.sgdbKeySet}
            ••••{coverArt.sgdbKeyHint}{coverArt.sgdbFromEnv ? ' (from STEAMGRIDDB_KEY)' : ''}
          {:else}
            not set
          {/if}
        </span>
      </div>
    </div>
    <div class="key-row">
      <input
        class="key-input"
        type="password"
        autocomplete="off"
        placeholder={coverArt.sgdbKeySet ? 'Replace key…' : 'Paste a SteamGridDB API key'}
        bind:value={sgdbInput}
      />
      <button class="btn btn-outline" disabled={!sgdbInput.trim()} onclick={() => saveSGDBKey(false)}>Save key</button>
      {#if coverArt.sgdbKeySet && !coverArt.sgdbFromEnv}
        <button class="btn btn-outline" onclick={() => saveSGDBKey(true)}>Remove</button>
      {/if}
    </div>
    <p class="section-hint">
      Free key: steamgriddb.com → Preferences → API. Shared with the CLI
      (<code>moxie config set steamgriddb-key</code>).
    </p>
    {#if coverArtMsg}<p class="section-hint">{coverArtMsg}</p>{/if}
  </section>

  <!-- ── Storage ────────────────────────────────────────── -->
  <section class="settings-section">
    <h3 class="section-title">Storage</h3>
    <div class="kv-list">
      <div class="kv-row">
        <span class="kv-key">Database</span>
        <span class="kv-value" title={dbPath}>{dbPath || '—'}</span>
      </div>
      <div class="kv-row">
        <span class="kv-key">Config directory</span>
        <span class="kv-value" title={configDir}>{configDir || '—'}</span>
      </div>
      <div class="kv-row">
        <span class="kv-key">Version</span>
        <span class="kv-value">{version || '—'}</span>
      </div>
    </div>
  </section>

  <!-- ── Application Update ─────────────────────────────── -->
  <!-- Full staged self-update flow (CheckForUpdate → DownloadUpdate with
       update:progress events → ApplyUpdate → restart). Previously imported
       in App.svelte but never mounted — this is its home. -->
  <section class="settings-section">
    <h3 class="section-title">Application Update</h3>
    <p class="section-hint">
      Download and install a new version of the Moxie desktop app.
    </p>
    <UpdateDialog
      version={appVersion}
      checking={appUpdateState.checking}
      info={appUpdateState.info}
      downloading={appUpdateState.downloading}
      downloadProgress={appUpdateState.downloadProgress}
      downloadComplete={appUpdateState.downloadComplete}
      error={appUpdateState.error}
      onCheck={onCheckAppUpdate}
      onDownload={onDownloadAppUpdate}
      onApply={onApplyAppUpdate}
    />
  </section>
</div>

<style>
  .settings-view {
    flex: 1;
    overflow: auto;
    padding: var(--space-8) var(--gutter);
    width: 100%;
    max-width: none;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-content: start;
    column-gap: var(--space-8);
  }

  .settings-header { margin-bottom: 24px; }
  .settings-header h2 { font-size: var(--text-2xl); font-weight: 700; margin: 0 0 4px; }
  .settings-subtitle { font-size: var(--text-base); color: var(--text-secondary); margin: 0; }

  .settings-section {
    margin-bottom: 32px;
    padding-bottom: 24px;
    border-bottom: 1px solid var(--border);
  }
  .settings-section:last-child { border-bottom: none; }

  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .section-title {
    font-size: var(--text-lg);
    font-weight: 600;
    margin: 0 0 4px;
    color: var(--text-primary);
  }

  .section-hint {
    font-size: var(--text-sm);
    color: var(--text-muted);
    margin: 0 0 12px;
  }

  .btn {
    padding: 6px 14px;
    font-size: var(--text-sm);
    border-radius: var(--radius-1);
    cursor: pointer;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-primary);
  }
  .btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-outline:hover:not(:disabled) { background: var(--bg-hover); }

  .dep-list { display: flex; flex-direction: column; gap: 4px; margin-top: 12px; }
  .dep-row {
    display: grid;
    grid-template-columns: 10px 150px 90px 1fr;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    font-size: var(--text-base);
  }
  .dep-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--warning);
  }
  .dep-dot.dep-ok { background: var(--success); }
  .dep-name { font-weight: 500; }
  .dep-status { font-size: var(--text-sm); color: var(--warning); }
  .dep-status.status-ok { color: var(--success); }
  .dep-details {
    font-size: var(--text-sm);
    color: var(--text-muted);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .seg { display: inline-flex; justify-self: start; width: max-content; border: 1px solid var(--border); }
  .seg-btn {
    min-width: 34px;
    padding: 4px 10px;
    border: none;
    border-left: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    cursor: pointer;
  }
  .seg-btn:first-child { border-left: none; }
  .seg-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .seg-btn.seg-active { background: var(--accent); color: var(--on-accent); }
  .error-text { color: var(--danger); }
  .kv-row.toggle-row { cursor: pointer; grid-template-columns: 1fr auto; }
  .toggle-row input { accent-color: var(--accent); width: 16px; height: 16px; justify-self: end; }
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
  .key-row { display: flex; gap: 8px; margin-top: 8px; }
  .key-input {
    flex: 1;
    padding: 6px 10px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    color: var(--text-primary);
    font-family: var(--font-mono);
  }
  .key-input:focus { outline: 1px solid var(--accent); }
  .kv-list { display: flex; flex-direction: column; gap: 4px; }
  .kv-row {
    display: grid;
    grid-template-columns: 150px 1fr;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
  }
  .kv-key { font-size: var(--text-base); color: var(--text-secondary); }
  .kv-value {
    font-size: var(--text-sm);
    font-family: var(--font-mono);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .error-box {
    margin-bottom: 16px;
    padding: 12px 16px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .error-box p { margin: 0; font-size: var(--text-base); color: var(--danger); }
  /* Two columns of sections on wide screens; header/errors span both. */
  @media (min-width: 1600px) {
    .settings-view { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .settings-header, .settings-view > .error-box { grid-column: 1 / -1; }
  }
</style>
