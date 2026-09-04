/**
 * Shared byte/throughput formatting — was previously copy-pasted into
 * DedupDialog.svelte, GameUpdatesView.svelte, and UpdateDialog.svelte with
 * slightly different rounding/edge-case handling in each copy. Single source
 * of truth now; import from here instead of reimplementing.
 */

/**
 * Formats a byte count as a human-readable string, e.g. "512 B", "3.4 MB".
 */
export function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.max(Math.floor(Math.log(bytes) / Math.log(1024)), 0), units.length - 1)
  const val = (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)
  return `${val} ${units[i]}`
}

/**
 * Formats a bytes-per-second rate as a human-readable string, e.g. "1.2 MB/s".
 */
export function formatSpeed(bps) {
  if (!bps || bps <= 0) return ''
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  const i = Math.min(Math.max(Math.floor(Math.log(bps) / Math.log(1024)), 0), units.length - 1)
  const val = (bps / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)
  return `${val} ${units[i]}`
}
