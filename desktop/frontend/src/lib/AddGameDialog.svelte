<script>
  import {PickDirectory, DetectGame, AddGame} from '../../wailsjs/go/main/App'

  let {
    onGameAdded = () => {},
  } = $props()

  // ── Engine color mapping (matches GameList / GameDetail) ─────
  function engineColor(engine) {
    const map = {
      "ren'py": '#ff6b9d', 'unity': '#6b9dff', 'rpg maker': '#9d6bff',
      'rpgmakermv': '#9d6bff', 'rpgmakermz': '#9d6bff',
      'rpgmakervxace': '#9d6bff',
      'html': '#ff9d6b', 'wolf rpg': '#6bff9d', 'flash': '#ff6b6b',
      'unreal': '#6bfffb', 'godot': '#9dff6b', 'electron': '#6bc9ff',
      'nw.js': '#6bc9ff',
    }
    for (const [key, c] of Object.entries(map)) {
      if (engine?.toLowerCase().includes(key)) return c
    }
    return '#9090a0'
  }

  // ── Common engines for override dropdown ─────────────────────
  const commonEngines = [
    "ren'py", "unity", "rpg maker", "rpgmakermv", "rpgmakermz",
    "html", "wolf rpg", "flash", "unreal", "godot", "electron", "nw.js",
  ]

  // ── State ─────────────────────────────────────────────────────
  let directoryPath = $state('')
  let detecting = $state(false)
  let detection = $state(null)          // DetectGame result or null
  let title = $state('')
  let engine = $state('')
  let version = $state('')
  let adding = $state(false)
  let result = $state(null)             // { success, gameId?, error? }
  let error = $state('')

  // ── Derived ───────────────────────────────────────────────────
  let canDetect = $derived(directoryPath.trim().length > 0)
  let hasDetection = $derived(detection !== null && !detecting)
  let canAdd = $derived(hasDetection && title.trim().length > 0 && !adding)

  // ── Directory Browse ──────────────────────────────────────────
  async function handleBrowse() {
    const dir = await PickDirectory()
    if (dir) {
      directoryPath = dir
      // Reset previous detection when path changes
      detection = null
      title = ''
      engine = ''
      version = ''
      result = null
      error = ''
    }
  }

  // ── Detect ────────────────────────────────────────────────────
  async function handleDetect() {
    const path = directoryPath.trim()
    if (!path) return

    detecting = true
    detection = null
    result = null
    error = ''

    try {
      const d = await DetectGame(path)
      detection = d
      title = d.title || ''
      engine = d.engine || ''
      version = d.version || ''

      // If detection returned an error field, show it
      if (d.error) {
        error = d.error
      }
    } catch (e) {
      error = String(e)
    } finally {
      detecting = false
    }
  }

  // ── Add Game ──────────────────────────────────────────────────
  async function handleAdd() {
    if (!canAdd) return

    adding = true
    result = null
    error = ''

    try {
      const gameId = await AddGame(directoryPath.trim(), title.trim(), engine.trim(), version.trim())
      result = {success: true, gameId}
      onGameAdded()
    } catch (e) {
      result = {success: false, error: String(e)}
    } finally {
      adding = false
    }
  }

  // ── Path changed externally (e.g. typed) → reset detection ──
  function handlePathInput() {
    detection = null
    title = ''
    engine = ''
    version = ''
    result = null
    error = ''
  }
</script>

<div class="add-game-dialog">
  <div class="add-game-header">
    <h2>Manually Add Game</h2>
    <p class="add-game-subtitle">Select a game directory, detect its engine, and add it to your library.</p>
  </div>

  <!-- ── Directory Input ──────────────────────────────────── -->
  <div class="dir-input-row">
    <input
      type="text"
      class="path-input"
      placeholder="/path/to/game/directory"
      bind:value={directoryPath}
      oninput={handlePathInput}
      onkeydown={(e) => e.key === 'Enter' && canDetect && handleDetect()}
    />
    <button class="btn btn-primary" onclick={handleBrowse}>
      Browse
    </button>
  </div>

  <!-- ── Detect Button ────────────────────────────────────── -->
  <div class="detect-row">
    <button
      class="btn btn-detect"
      onclick={handleDetect}
      disabled={!canDetect || detecting}
    >
      {#if detecting}
        <span class="spinner"></span>
        Detecting…
      {:else}
        Detect Engine
      {/if}
    </button>
  </div>

  <!-- ── Detecting Spinner ────────────────────────────────── -->
  {#if detecting}
    <div class="detecting-section">
      <div class="spinner"></div>
      <p>Scanning directory and detecting engine…</p>
    </div>
  {/if}

  <!-- ── Detection Result ─────────────────────────────────── -->
  {#if hasDetection}
    <div class="preview-section">
      <h3 class="preview-title">Game Details</h3>

      <!-- Title -->
      <div class="field-group">
        <label class="field-label" for="add-title">Title</label>
        <input
          id="add-title"
          type="text"
          class="field-input"
          placeholder="Game title"
          bind:value={title}
        />
      </div>

      <!-- Engine -->
      <div class="field-group">
        <label class="field-label" for="add-engine">Engine</label>
        <div class="engine-row">
          <span class="engine-badge" style="--ec: {engineColor(engine)}">{engine || 'Unknown'}</span>
          <select
            id="add-engine"
            class="field-select"
            bind:value={engine}
          >
            <option value="">— Custom —</option>
            {#each commonEngines as eng}
              <option value={eng}>{eng}</option>
            {/each}
          </select>
        </div>
      </div>

      <!-- Version -->
      <div class="field-group">
        <label class="field-label" for="add-version">Version</label>
        <input
          id="add-version"
          type="text"
          class="field-input"
          placeholder="e.g. 1.0"
          bind:value={version}
        />
      </div>

      <!-- Size (read-only) -->
      {#if detection.sizeLabel}
        <div class="field-group">
          <span class="field-label">Size</span>
          <p class="field-readonly">{detection.sizeLabel}</p>
        </div>
      {/if}

      <!-- Path (read-only) -->
      <div class="field-group">
        <span class="field-label">Directory</span>
        <p class="field-readonly field-mono">{detection.path || directoryPath}</p>
      </div>

      <!-- Add button -->
      <div class="add-row">
        <button
          class="btn btn-primary btn-add"
          onclick={handleAdd}
          disabled={!canAdd}
        >
          {#if adding}
            <span class="spinner"></span>
            Adding…
          {:else}
            Add Game
          {/if}
        </button>
      </div>
    </div>
  {/if}

  <!-- ── Error from detection ─────────────────────────────── -->
  {#if error && !result}
    <div class="error-section">
      <p class="error-title">Detection failed:</p>
      <p class="error-line">{error}</p>
    </div>
  {/if}

  <!-- ── Result ───────────────────────────────────────────── -->
  {#if result}
    {#if result.success}
      <div class="result-section">
        <h3 class="result-title">Game Added Successfully</h3>
        <p class="result-detail">Game ID: {result.gameId}</p>
        <p class="result-detail">"{title}" has been added to your library.</p>
      </div>
    {:else}
      <div class="error-section">
        <p class="error-title">Failed to add game:</p>
        <p class="error-line">{result.error}</p>
      </div>
    {/if}
  {/if}
</div>

<style>
  .add-game-dialog {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 720px;
    margin: 0 auto;
    width: 100%;
  }

  .add-game-header {
    margin-bottom: 24px;
  }
  .add-game-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .add-game-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Directory Input ──────────────────── */
  .dir-input-row {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
  }
  .path-input {
    flex: 1;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
  }
  .path-input:focus { border-color: var(--accent); }

  .btn {
    padding: 7px 16px;
    border: none;
    border-radius: 6px;
    font-size: 13px;
    cursor: pointer;
    white-space: nowrap;
    transition: all 0.12s;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .btn-primary {
    background: var(--accent);
    color: #fff;
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }

  /* ── Detect Row ───────────────────────── */
  .detect-row {
    margin-bottom: 20px;
  }
  .btn-detect {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent);
  }
  .btn-detect:hover:not(:disabled) { background: var(--accent); color: #fff; }

  /* ── Detecting spinner ────────────────── */
  .detecting-section {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
    font-size: 13px;
    color: var(--text-secondary);
  }

  .spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* ── Preview Section ──────────────────── */
  .preview-section {
    margin: 16px 0;
    padding: 20px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
  }
  .preview-title {
    font-size: 15px;
    font-weight: 600;
    margin: 0 0 16px;
    color: var(--text-primary);
  }

  .field-group {
    margin-bottom: 14px;
  }
  .field-group:last-of-type {
    margin-bottom: 0;
  }
  .field-label {
    display: block;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    margin-bottom: 4px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .field-input {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
    box-sizing: border-box;
  }
  .field-input:focus { border-color: var(--accent); }

  .field-select {
    padding: 7px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
    cursor: pointer;
  }
  .field-select:focus { border-color: var(--accent); }

  .field-readonly {
    font-size: 13px;
    color: var(--text-primary);
    margin: 0;
    padding: 6px 0;
  }
  .field-mono {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text-secondary);
  }

  /* ── Engine Row (badge + dropdown) ────── */
  .engine-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .engine-badge {
    padding: 2px 10px;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
    white-space: nowrap;
  }

  /* ── Add Row ──────────────────────────── */
  .add-row {
    margin-top: 20px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }
  .btn-add {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 120px;
    justify-content: center;
  }

  /* ── Result ───────────────────────────── */
  .result-section {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--success);
    border-radius: 8px;
    background: color-mix(in srgb, var(--success) 8%, transparent);
  }
  .result-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--success);
    margin: 0 0 6px;
  }
  .result-detail {
    font-size: 13px;
    color: var(--text-primary);
    margin: 0 0 4px;
  }
  .result-detail:last-child {
    margin-bottom: 0;
  }

  .error-section {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--danger);
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .error-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--danger);
    margin: 0 0 4px;
  }
  .error-line {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 0;
    font-family: var(--font-mono);
  }
</style>
