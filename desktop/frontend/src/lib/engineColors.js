/**
 * Canonical engine color map — shared across all desktop components.
 *
 * These colors match the TUI palette in internal/tui/styles.go and are
 * designed to be visually distinct on both dark and light backgrounds.
 *
 * Usage: import { engineColor, engineStyle } from './engineColors.js'
 *
 *   engineColor(name)     → hex color string (for CSS color or var(--ec))
 *   engineStyle(name)     → inline style string (for F95Browser badge)
 */

// Map of canonical engine name → hex color
const colorMap = {
  // Canonical engines (matching internal/engine/detector.go)
  Unity:        '#00bcd4', // cyan
  RenPy:        '#e91e90', // pink/magenta
  RPGM:         '#4caf50', // green
  UnrealEngine: '#ffc107', // amber/yellow
  HTML:         '#2196f3', // blue
  Flash:        '#ff9800', // orange
  Java:         '#795548', // brown
  ADRIFT:       '#7c4dff', // deep purple accent
  QSP:          '#009688', // teal
  RAGS:         '#e65100', // deep orange
  Tads:         '#0097a7', // dark cyan
  WebGL:        '#03a9f4', // light blue
  WolfRPG:      '#ff6d00', // warm orange
  Others:       '#9090a0', // gray

  // Non-canonical but commonly used engine aliases
  'ren\'py':    '#e91e90', // RenPy lowercase with apostrophe
  'rpg maker':  '#4caf50', // RPGM descriptive name
  'wolf rpg':   '#ff6d00', // WolfRPG descriptive name
  electron:     '#26c6da', // cyan-ish (Electron/nw.js)
  'nw.js':      '#26c6da', // same as electron
  godot:        '#66bb6a', // green-tinted
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
  godot:        'godot',
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
