/**
 * Library sort/filter/predicate helpers — extracted out of GameList.svelte,
 * which mixed this pure logic in with rendering and virtualization glue.
 *
 * `sortCompare`/`toggleSort`/`sortIcon`/`onSortSelect` read/write
 * `library.sortColumn`/`library.sortDesc` directly (same convention as
 * cover.js reading/writing shared viewState) rather than taking getters —
 * there's exactly one arrangement axis, shared across tabs.
 */
import {library} from './viewState.svelte.js'

// ── Quick views (P0-4) ────────────────────────────────────────
export const QUICK_VIEWS = [
  {value: 'all',       label: 'All'},
  {value: 'installed', label: 'Installed'},
  {value: 'ready',     label: 'Ready to play'},
  {value: 'recent',    label: 'Recently played'},
]

const VIRTUAL_PATH = '/virtual/'

export function isVirtual(g) {
  return !!g?.path?.startsWith(VIRTUAL_PATH)
}

export function tsVal(g, key) {
  const s = g?.[key]
  if (!s) return null
  const t = Date.parse(s)
  return Number.isNaN(t) ? null : t
}

// `busyIds` is the caller's (GameList's) set of game ids currently being
// mutated by an update/install pipeline — see GameList.svelte's
// `UPDATE_BUSY_PHASES` effect for how it's derived from `gameStates`.
export function quickViewMatches(quickView, g, busyIds) {
  switch (quickView) {
    case 'installed': return !isVirtual(g)
    case 'ready':     return !isVirtual(g) && !busyIds.has(String(g.id))
    case 'recent':    return tsVal(g, 'lastPlayed') !== null
    default:          return true
  }
}

// ── Sorting / arrangement ─────────────────────────────────────
// Default arrangement is recency-first (P0-2): games with a last-played
// timestamp sort most-recent first, never-played games group last (title
// asc). "Date added" is a first-class option — moxie owns created_at,
// which competitors like Heroic can't even offer. Timestamp fields are
// optional on the summary (see mock-data.js enrichment note); missing
// values degrade to stable fallbacks instead of breaking the sort.
export const SORT_OPTIONS = [
  {value: 'recent', label: 'Recently played'},
  {value: 'added',  label: 'Date added'},
  {value: 'title',  label: 'Title A–Z'},
  {value: 'engine', label: 'Engine'},
  {value: 'version',label: 'Version'},
  {value: 'size',   label: 'Size'},
  {value: 'status', label: 'Status'},
]

// Numeric-aware version comparison: "10.0" sorts after "9.0". Splits on
// non-alphanumerics and compares token-wise — numeric tokens numerically,
// anything else lexically (numeric tokens first when mixed).
export function compareVersions(a, b) {
  const ta = String(a || '')
  const tb = String(b || '')
  if (ta === tb) return 0
  const pa = ta.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
  const pb = tb.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
  const len = Math.max(pa.length, pb.length)
  for (let i = 0; i < len; i++) {
    const x = pa[i]
    const y = pb[i]
    if (x === undefined) return -1
    if (y === undefined) return 1
    const xNum = /^\d+$/.test(x)
    const yNum = /^\d+$/.test(y)
    if (xNum && yNum) {
      const n = parseInt(x, 10) - parseInt(y, 10)
      if (n !== 0) return n
    } else if (xNum !== yNum) {
      return xNum ? -1 : 1
    } else {
      const c = x.localeCompare(y)
      if (c !== 0) return c
    }
  }
  return 0
}

export function toggleSort(col) {
  if (library.sortColumn === col) {
    library.sortDesc = !library.sortDesc
  } else {
    library.sortColumn = col
    library.sortDesc = false
  }
}

export function sortIcon(col) {
  if (library.sortColumn !== col) return '▽'
  return library.sortDesc ? '▲' : '▼'
}

export function onSortSelect(e) {
  library.sortColumn = e.target.value
  // Recency & date-added default to most-recent-first; column sorts start
  // ascending (clicking a header toggles direction).
  library.sortDesc = e.target.value === 'added'
}

export function sortCompare(a, b) {
  switch (library.sortColumn) {
    case 'recent': {
      const ta = tsVal(a, 'lastPlayed')
      const tb = tsVal(b, 'lastPlayed')
      if (ta === null && tb === null) {
        return (a.title || '').localeCompare(b.title || '', undefined, {numeric: true})
      }
      if (ta === null) return 1
      if (tb === null) return -1
      return tb - ta                     // most recent first
    }
    case 'added': {
      const ta = tsVal(a, 'createdAt')
      const tb = tsVal(b, 'createdAt')
      let cmp
      if (ta === null && tb === null) cmp = (a.id || 0) - (b.id || 0)
      else if (ta === null) cmp = 1
      else if (tb === null) cmp = -1
      else cmp = ta - tb
      return library.sortDesc ? -cmp : cmp
    }
    case 'title':
      return (a.title || '').localeCompare(b.title || '', undefined, {numeric: true})
    case 'engine':
      return (a.engine || '').localeCompare(b.engine || '')
    case 'version':
      return compareVersions(a.version, b.version)
    case 'status':
      return (a.status || '').localeCompare(b.status || '')
    case 'size':
      return (a.sizeBytes || 0) - (b.sizeBytes || 0)
    default:
      return 0
  }
}

// ── Per-game display helpers (shared by grid card + table row) ─
export function hasUpdate(game) {
  return game.latestVersion && game.version &&
         game.latestVersion !== game.version
}

export function lastPlayedDate(g) {
  const t = tsVal(g, 'lastPlayed')
  return t === null ? null : new Date(t)
}

export function relativePlayed(d) {
  const mins = Math.floor((Date.now() - d.getTime()) / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  const months = Math.floor(days / 30)
  if (months < 12) return `${months}mo ago`
  return `${Math.floor(months / 12)}y ago`
}
