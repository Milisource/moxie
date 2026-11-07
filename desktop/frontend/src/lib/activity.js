// One vocabulary for "what is moxie doing to this game right now" across
// every tile in the library. The Updates view owns the detailed progress UI;
// the library only needs a compact, consistent badge.
//
// Phase names come from two places: App's gameStates (update pipeline) and
// App's installState (add-from-browser install pipeline). They overlap
// (`downloading`, `extracting`, `selecting-link`) but not completely, so the
// mapping stays explicit rather than clever.

const SHORT_LABEL = {
  queued: 'Queued',
  syncing: 'Syncing',
  'selecting-link': 'Finding link',
  downloading: 'Downloading',
  extracting: 'Extracting',
  merging: 'Merging',
  'updating-db': 'Finishing',
  installing: 'Installing',
}

const ACTIVE_PHASES = new Set([
  'queued', 'syncing', 'selecting-link', 'downloading',
  'extracting', 'merging', 'updating-db', 'installing',
])

function clampPercent(value) {
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) return null
  return Math.min(100, Math.round(n))
}

/**
 * Returns null when the game is idle, otherwise:
 *   {label, tone: 'busy'|'queued'|'error', percent: number|null, detail}
 * `percent` is 0-100 or null when the phase has no measurable progress.
 */
export function activityFor(gameId, gameState, installState) {
  const installing = installState?.running && Number(installState.gameId) === Number(gameId)
  if (installing) {
    const phase = installState.phase || ''
    // progress is download-only; extracting/installing would otherwise keep
    // showing "100%" while work is clearly still happening.
    const percent = phase === 'downloading' ? clampPercent(installState.progress) : null
    const label = SHORT_LABEL[phase] || 'Installing'
    return {
      label,
      tone: 'busy',
      percent,
      detail: percent !== null ? `${label} ${percent}%` : label,
    }
  }

  const phase = gameState?.phase || 'idle'
  if (gameState?.phase === 'error') {
    return {label: 'Failed', tone: 'error', percent: null, detail: gameState.error || 'Update failed'}
  }
  if (!ACTIVE_PHASES.has(phase)) return null

  const percent = phase === 'downloading' ? clampPercent(gameState.percent) : null
  return {
    label: SHORT_LABEL[phase] || 'Working',
    tone: phase === 'queued' ? 'queued' : 'busy',
    percent,
    detail: percent !== null ? `${percent}%` : (SHORT_LABEL[phase] || 'Working'),
  }
}
