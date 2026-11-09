// Canonical game statuses — mirrors the backend's valid status set. Every
// view (filter dropdowns, context menus, status badges, selects) should read
// from here instead of re-declaring its own list so the UI can't drift from
// the database CHECK constraint.
export const GAME_STATUSES = ['active', 'completed', 'abandoned', 'on_hold', 'unknown']

export function statusLabel(s) {
  const labels = {
    active: 'Active',
    completed: 'Completed',
    abandoned: 'Abandoned',
    on_hold: 'On Hold',
    unknown: 'Unknown',
  }
  return labels[s] || s || 'Unknown'
}
