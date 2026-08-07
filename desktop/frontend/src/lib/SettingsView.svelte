<script>
  import {onMount} from 'svelte'
  import {
    CheckDependencies,
    GetDbPath,
    GetConfigDir,
    GetVersion,
  } from '../../wailsjs/go/main/App'
  import ScanPaths from './ScanPaths.svelte'

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
</div>

<style>
  .settings-view {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 760px;
    margin: 0 auto;
    width: 100%;
  }

  .settings-header { margin-bottom: 24px; }
  .settings-header h2 { font-size: 20px; font-weight: 700; margin: 0 0 4px; }
  .settings-subtitle { font-size: 13px; color: var(--text-secondary); margin: 0; }

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
    font-size: 15px;
    font-weight: 600;
    margin: 0 0 4px;
    color: var(--text-primary);
  }

  .section-hint {
    font-size: 12px;
    color: var(--text-muted);
    margin: 0 0 12px;
  }

  .btn {
    padding: 6px 14px;
    font-size: 12px;
    border-radius: 6px;
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
    border-radius: 6px;
    background: var(--bg-secondary);
    font-size: 13px;
  }
  .dep-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--warning);
  }
  .dep-dot.dep-ok { background: var(--success); }
  .dep-name { font-weight: 500; }
  .dep-status { font-size: 12px; color: var(--warning); }
  .dep-status.status-ok { color: var(--success); }
  .dep-details {
    font-size: 12px;
    color: var(--text-muted);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kv-list { display: flex; flex-direction: column; gap: 4px; }
  .kv-row {
    display: grid;
    grid-template-columns: 150px 1fr;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-secondary);
  }
  .kv-key { font-size: 13px; color: var(--text-secondary); }
  .kv-value {
    font-size: 12px;
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
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .error-box p { margin: 0; font-size: 13px; color: var(--danger); }
</style>
