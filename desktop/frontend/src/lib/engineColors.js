/**
 * Canonical engine color map — shared with the TUI.
 *
 * The single source of truth is internal/engine/engine-colors.json (embedded
 * into the Go binary and imported here). Colors mirror F95Zone's "Latest
 * Updates" page: engine→class mapping from the site's latestUpdates prefix
 * data, class→color from its stylesheet (see internal/engine/colors.go for
 * the class reference table).
 *
 * Usage: import { engineColor, engineStyle } from './engineColors.js'
 *
 *   engineColor(name)     → hex color string (for CSS color or var(--ec))
 *   engineStyle(name)     → inline style string (for F95Browser badge)
 */

import colors from '../../../../internal/engine/engine-colors.json'

// Map of canonical engine name → hex color (from engine-colors.json)
const colorMap = {
  ...colors,

  // Non-canonical engines with no F95Zone color of their own
  electron: '#26c6da', // cyan-ish (Electron/nw.js)
  'nw.js':  '#26c6da', // same as electron

  // Descriptive aliases of canonical engines
  'ren\'py':   colors.RenPy,
  'rpg maker': colors.RPGM,
  'wolf rpg':  colors.WolfRPG,
  godot:       colors.Godot,
}

// Lookup: try exact match first, then substring-of-name, then alias list
const aliasMap = {
  unity:        'Unity',
  renpy:        'RenPy',
  'ren\'py':    'RenPy',
  rpgm:         'RPGM',
  'rpg maker':  'RPGM',
  rpgmakermv:   'RPGM',
  rpgmakermz:   'RPGM',
  rpgmakervxace:'RPGM',
  unreal:       'UnrealEngine',
  unrealengine: 'UnrealEngine',
  html:         'HTML',
  flash:        'Flash',
  java:         'Java',
  adrift:       'ADRIFT',
  qsp:          'QSP',
  rags:         'RAGS',
  tads:         'Tads',
  webgl:        'WebGL',
  wolfrpg:      'WolfRPG',
  'wolf rpg':   'WolfRPG',
  others:       'Others',
  electron:     'electron',
  'nw.js':      'nw.js',
  godot:        'Godot',
}

/**
 * Returns a hex color string for the given engine name.
 * Falls back to '#9090a0' (gray) for unknown engines.
 */
export function engineColor(engine) {
  if (!engine) return '#9090a0'

  const key = engine.toLowerCase().trim()

  // Direct lookup in alias map
  const canonical = aliasMap[key]
  if (canonical && colorMap[canonical]) return colorMap[canonical]

  // Fallback: check if any alias is a substring of the given name
  for (const [alias, canonicalName] of Object.entries(aliasMap)) {
    if (key.includes(alias)) {
      const c = colorMap[canonicalName]
      if (c) return c
    }
  }

  return '#9090a0'
}

/**
 * Returns an inline CSS style string for an engine badge.
 * Includes background and text color.
 */
export function engineStyle(engine) {
  const c = engineColor(engine)
  if (c === '#9090a0') return '' // unknown — no special style needed
  return `background: ${c}22; color: ${c}; border-color: ${c}44`
}

/**
 * Returns the list of all known canonical engine names.
 */
export function engineNames() {
  return Object.keys(colorMap).filter(k => k === k[0].toUpperCase() + k.slice(1))
}

/**
 * Returns every engine value that may be stored in the database — the
 * canonical detector names (Unity, RenPy, Java, QSP, Tads, …) plus the
 * lowercase/descriptive aliases (unity, ren'py, rpgmakermv, …) that manual
 * entry or older data may have written. Sorted case-insensitively so the
 * dropdown order is stable.
 */
export function engineOptions() {
  const set = new Set([...Object.keys(colorMap), ...Object.keys(aliasMap)])
  return [...set].sort((a, b) => a.localeCompare(b, undefined, {sensitivity: 'base'}))
}
