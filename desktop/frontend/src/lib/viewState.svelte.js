// Tab-surviving view state.
//
// Views are destroyed on every tab switch (App.svelte renders them inside
// {#if} blocks), so anything interactive that must survive navigation lives
// here as module-scope state: filters, sort order, scroll offsets, in-flight
// results. Long-running operations (sync, scan, covers, updates) already live
// at App level in App.svelte — this module covers the per-view UI state.
//
// Why objects and not individual exports: the Svelte compiler only rewrites
// runes inside one file at a time, so a directly exported `$state` binding
// can't be reassigned from an importing component ("Cannot assign to
// import"). Exporting one stateful object per view keeps property writes
// reactive across modules.

// Reads a persisted string preference, falling back when unset, unreadable
// (private-browsing style storage exceptions), or holding a stale/foreign
// value outside the allowed set (schema drift between app versions).
function readStored(key, fallback, allowed) {
  try {
    const v = localStorage.getItem(key)
    return allowed.includes(v) ? v : fallback
  } catch {
    return fallback
  }
}

function writeStored(key, value) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Storage unavailable/full — the preference just won't survive restart.
  }
}

// Reads a persisted JSON value, falling back when unset, unreadable, or
// holding malformed JSON (schema drift between app versions / hand-edited
// storage).
function readStoredJSON(key, fallback) {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return fallback
    return JSON.parse(raw)
  } catch {
    return fallback
  }
}

function writeStoredJSON(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Storage unavailable/full — the value just won't survive restart.
  }
}

const VIEW_MODE_KEY = 'moxie:library-view-mode'
const DENSITY_KEY = 'moxie:library-density'
const SMART_COLLECTIONS_KEY = 'moxie:smart-collections'
const LAST_SYNC_KEY = 'moxie:last-sync'

// ── Library list ───────────────────────────────────────────────
export const library = $state({
  search: '',
  engine: 'All',
  status: '',

  // P0 library-as-a-collection surface (docs/desktop-ui-research.md §7):
  //   quickView — primary play-state tabs
  //     'all' | 'installed' | 'ready' | 'recent'
  //   viewMode — grid is the default (Heroic/Playnite-style); list stays as
  //     an explicit toggle for the data-table crowd. Persisted (P1 item 7)
  //     so the user's last choice survives an app restart.
  //     'grid' | 'list'
  //   density — row/card sizing (P1 item 7). Persisted alongside viewMode.
  //     'comfortable' | 'compact'
  //   sortColumn — arrangement. Defaults to 'recent' (recency first, §7 P0-2);
  //     'added' sorts by date added (moxie owns created_at); the rest are the
  //     classic column sorts kept for the list view's header.
  //     'recent' | 'added' | 'title' | 'engine' | 'version' | 'size' | 'status'
  quickView: 'all',
  viewMode: readStored(VIEW_MODE_KEY, 'grid', ['grid', 'list']),
  density: readStored(DENSITY_KEY, 'comfortable', ['comfortable', 'compact']),
  sortColumn: 'recent',
  sortDesc: false,
  scrollTop: 0,
  gridScrollTop: 0,

  // Games whose cover <img> failed to load + a retry epoch: a remounted list
  // re-renders cached 404s from the webview without re-requesting them, so
  // the failure set survives too. A sync/backfill bumps the epoch to force
  // retries.
  failedCovers: new Set(),
  coverEpoch: 0,
})

// Setters (rather than direct `library.viewMode = ...` assignment) so the
// persisted preference stays in sync with every write site.
export function setViewMode(mode) {
  library.viewMode = mode
  writeStored(VIEW_MODE_KEY, mode)
}

export function setDensity(density) {
  library.density = density
  writeStored(DENSITY_KEY, density)
}

// ── F95 browser ────────────────────────────────────────────────
export const browser = $state({
  query: '',
  results: [],
  loading: false,
  searched: false,
  error: '',
  selected: null,
  previewing: false,
  preview: null,
  previewError: '',
  expandedOverview: false,
  addResult: null,

  // Shared add-to-library in-flight flag: survives remounts so a mid-add
  // tab switch can't re-enable the button and double-add the same game.
  addingInFlight: false,

  // Monotonic request ids shared across mounts: a stale in-flight response
  // from a destroyed instance must never land in a freshly remounted view.
  // Only a NEWER request (which bumps the shared counter) supersedes an
  // older one — exactly as if the view had never unmounted.
  searchSeq: 0,
  previewSeq: 0,

  // Whether the most recent search/preview request is still awaiting its
  // backend response. Survives remounts so a fresh instance can tell whether
  // the loading/previewing flags it inherited reflect a genuinely in-flight
  // request (spinner must keep spinning) or an orphaned flag (safe to clear).
  searchInFlight: false,
  previewInFlight: false,
})

// ── Downloads view ─────────────────────────────────────────────
export const downloads = $state({
  search: '',
  expanded: new Set(),
})

// ── Collections view ───────────────────────────────────────────
export const collectionsView = $state({
  selectedId: null,
  selectedSmartId: null,

  // Smart collections (P1 item 6, docs/desktop-ui-research.md §7): a rule of
  // {field: 'engine'|'status', value} rather than a stored game list. Scoped
  // to the frontend only — no backend table/migration — so membership is
  // recomputed client-side from the currently loaded game list and never
  // needs to be kept in sync with server state. Persisted so rules survive
  // an app restart the same way viewMode/density do.
  smartCollections: readStoredJSON(SMART_COLLECTIONS_KEY, []),
})

export function addSmartCollection(field, value) {
  const rule = {id: `${field}:${value}`, field, value}
  if (collectionsView.smartCollections.some((r) => r.id === rule.id)) return
  collectionsView.smartCollections = [...collectionsView.smartCollections, rule]
  writeStoredJSON(SMART_COLLECTIONS_KEY, collectionsView.smartCollections)
}

export function removeSmartCollection(id) {
  collectionsView.smartCollections = collectionsView.smartCollections.filter((r) => r.id !== id)
  writeStoredJSON(SMART_COLLECTIONS_KEY, collectionsView.smartCollections)
  if (collectionsView.selectedSmartId === id) collectionsView.selectedSmartId = null
}

// ── App-level meta (status bar, P2 item 11) ─────────────────────
// lastSyncAt has no backend-persisted equivalent (SyncAllGames doesn't
// record a timestamp anywhere durable) — it's set from the sync:complete
// event and persisted here so "last synced" survives a restart the same way
// a real sync-history table would, without needing one.
export const appMeta = $state({
  lastSyncAt: (() => {
    try { return localStorage.getItem(LAST_SYNC_KEY) || '' } catch { return '' }
  })(),
})

export function setLastSyncAt(iso) {
  appMeta.lastSyncAt = iso
  writeStored(LAST_SYNC_KEY, iso)
}
