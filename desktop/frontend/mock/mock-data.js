// ─────────────────────────────────────────────────────────────
// Mock fixture data for standalone-browser development/screenshots.
// Mirrors the JSON shapes from desktop/app.go (DesktopGameSummary,
// DesktopGameDetail, DesktopDownloadLink, DesktopDownloadLinkWithGame,
// F95SearchResult, ThreadPreview). Never shipped — dev-only entry point.
// ─────────────────────────────────────────────────────────────
//
// Enrichment note (keep in sync with the Go data model):
// Each game carries `createdAt` (RFC3339, mirrors db.Game.CreatedAt) and
// `lastPlayed` (RFC3339 or null, mirrors DesktopPlayEntry.PlayedAt). These
// power the library's recency-first default arrangement and "date added"
// sort. The real DesktopGameSummary does not yet serialize them — the
// frontend reads them when present and degrades gracefully when absent —
// so the fixtures are a superset of what the backend returns today while
// staying faithful to real Go shapes. Paths starting with `/virtual/`
// mirror db.VirtualPathPrefix (F95Zone references not yet downloaded).

const P = (id, title, engine, version, latestVersion, status, sizeLabel, sizeBytes, hasCover = true, developer = 'Pixel Bloom Studio', overview = '', createdAt = '', lastPlayed = null) => ({
  id, title, engine, version, latestVersion, status,
  path: `/games/${title.replace(/\W+/g, '_')}`,
  exePath: '',
  sizeBytes, sizeLabel,
  hasCover,
  developer,
  createdAt,
  lastPlayed,
  overview: overview || `${title} is a ${engine} game about choices, relationships, and the consequences that follow. The story branches based on the decisions you make, with multiple endings and a large cast of characters.`,
})

export const GAMES = [
  P(1,  'Ember Falls',       'Ren\'Py', '1.4.2', '1.4.2', 'active',    '2.6 GB', 2791728742, true, 'Pixel Bloom Studio', '', '2026-01-12T18:00:00Z', '2026-09-01T20:10:00Z'),
  P(2,  'Starforge',         'Unity',   '0.12.0','0.14.1','active',    '9.1 GB', 9771050598, true, 'Aether Forge Interactive', '', '2026-02-03T11:30:00Z', '2026-08-28T19:40:00Z'),
  P(3,  'The Tides of Aurel', 'RPGM',   '3.2',   '3.4.1', 'completed', '1.9 GB', 2040109465, true, 'Saltmarsh Works', '', '2025-11-20T09:15:00Z', '2026-05-03T14:20:00Z'),
  P(4,  'Midnight Protocol',  'Unity',   '2.0.5', '2.0.5', 'active',    '4.3 GB', 4617089843, true, 'Relay Nine Games', '', '2026-03-15T22:45:00Z', '2026-09-02T21:14:00Z'),
  P(5,  'Crimson Harvest',    'HTML',    '0.9.3', '1.0.0', 'on_hold',   '640 MB', 671088640, true, 'Emberlight Interactive', '', '2026-04-01T08:00:00Z', null),
  P(6,  'Glass Garden',       'Ren\'Py', '0.7.0', '0.7.0', 'active',    '1.2 GB', 1288490188, true, 'Bloomfield Soft', '', '2026-04-22T17:05:00Z', '2026-08-30T20:30:00Z'),
  P(7,  'Voidwalker',         'Unity',   '0.4.1', '0.5.2', 'abandoned', '7.8 GB', 8375186227, true, 'Null Protocol', '', '2026-05-10T13:25:00Z', null),
  P(8,  'Paper Crowns',       'RPGM',    '1.0',   '1.0',   'completed', '860 MB', 901775360, true, 'Gilded Quill', '', '2025-12-05T16:40:00Z', '2026-07-18T19:05:00Z'),
  P(9,  'Solace',             'Ren\'Py', '2.1.0', '2.3.0', 'active',    '3.4 GB', 3650722201, true, 'Harbor Light Games', '', '2026-06-01T10:55:00Z', '2026-09-03T09:12:00Z'),
  P(10, 'Ironclad Hearts',    'UE4',     '0.6.2', '0.6.2', 'active',    '14.2 GB',15247133573, true, 'Forge & Anvil', '', '2026-06-14T21:10:00Z', null),
  P(11, 'Whisperwood',        'HTML',    '0.3.0', '0.4.0', 'on_hold',   '220 MB', 230686720,  true, 'Mosslight', '', '2026-06-28T07:35:00Z', null),
  P(12, 'Neon City Nights',   'Godot',   '1.2.3', '1.2.3', 'active',    '1.6 GB', 1717986918, true, 'Retrowave Studio', '', '2026-07-05T23:50:00Z', '2026-08-25T22:00:00Z'),
  P(13, 'The Alchemist\'s Daughter','Ren\'Py','5.0','5.0',   'completed', '5.7 GB', 6120328396, true, 'Transmutation Works', '', '2026-01-30T14:20:00Z', null),
  P(14, 'Frostfall',          'RPGM',    '0.8.0', '0.9.1', 'active',    '2.2 GB', 2362232012, true, 'Northpine Games', '', '2026-07-20T12:15:00Z', '2026-08-29T18:45:00Z'),
  P(15, 'Horizon Divide',     'Unity',   '1.0.0', '1.1.0', 'active',    '11.0 GB',11811160064, true, 'Boundary Nine', '', '2026-02-18T09:30:00Z', null),
  P(16, 'Starlit Sky',        'Ren\'Py', '4.2',   '4.2',   'completed', '4.0 GB', 4294967296, true, 'Celestial Forge', '', '2025-10-14T19:00:00Z', '2026-06-10T20:15:00Z'),
  P(17, 'Dust & Echoes',      'WolfRPG', '1.1',   '1.1',   'unknown',   '380 MB', 398458880,  true, 'Remote Signal', '', '2026-08-02T11:40:00Z', null),
  P(18, 'Gilded Chains',      'Unity',   '0.10.0','0.11.0','active',    '6.2 GB', 6657199308, true, 'Orichalcum', '', '2026-08-10T15:30:00Z', '2026-09-01T17:25:00Z'),
  P(19, 'Running with Scissors','HTML',  '2.0',   '2.0',   'abandoned', '150 MB', 157286400,  true, 'Quick Cut', '', '2026-08-18T08:55:00Z', null),
  P(20, 'The Longest Night',  'Ren\'Py', '0.5.4', '0.5.4', 'active',    '2.9 GB', 3113851289, true, 'Dusklight', '', '2026-08-25T20:10:00Z', null),
  // F95Zone reference not yet downloaded — mirrors db.VirtualPathPrefix.
  P(22, 'Ashes of Aurora',    'Ren\'Py', '',      '1.0.0', 'active',   '—',       0,          true, 'Fawnvale', '', '2026-09-03T08:00:00Z', null),
]
// Mark the virtual game's path explicitly (P() sets a real-looking path).
GAMES[20].path = '/virtual/f95zone/229833'
GAMES[20].exePath = ''

export const GAME_DETAILS = {
  1: P(1, 'Ember Falls', 'Ren\'Py', '1.4.2', '1.4.2', 'active', '2.6 GB', 2791728742, true, 'Vesper Games',
      'A slow-burn romance visual novel set in a mountain town where the winter never ends. Three love interests, a town council conspiracy, and a past that refuses to stay buried.'),
  2: P(2, 'Starforge', 'Unity', '0.12.0', '0.14.1', 'active', '9.1 GB', 9771050598, true, 'Aether Forge Interactive',
      'Open-world colony survival on a shattered planet. Build, farm, and defend against the crystal storms while unraveling what destroyed the old civilization.'),
  3: P(3, 'The Tides of Aurel', 'RPGM', '3.2', '3.4.1', 'completed', '1.9 GB', 2040109465, true, 'Saltmarsh Works',
      'A complete RPG Maker adventure about a lighthouse keeper who must sail between islands to restore the tides. 40+ hours of content.'),
}

export const GAME_TAGSETS = {
  1: ['Visual Novel', 'Romance', 'Drama', 'Sandbox', 'Female Protagonist'],
  2: ['3D', 'Survival', 'Open World', 'Base Building', 'Male Protagonist'],
  3: ['Turn-Based', 'Fantasy', 'Female Protagonist', 'Adventure'],
  22: ['Visual Novel', 'Fantasy', 'Female Protagonist'],
}

// Collage-tile edge cases (Phase 3, desktop/desktop-ui-research.md §7 item 6):
// 'Visual Novels' has 6 members (> the 4-cover collage cap, exercises the
// "+N" overlay), 'Need Progress' has exactly 2, 'Solo Pick' has exactly 1
// (single cover fills the whole tile), 'Someday' has 0 (empty-tile
// placeholder). gameCount intentionally mirrors COLLECTION_GAMES length
// rather than being independently made up, like the real backend's count.
export const COLLECTIONS = [
  {id: 1, name: 'Visual Novels',  description: 'Story-first games',   gameCount: 6},
  {id: 2, name: 'Finished',       description: 'Playable start to finish', gameCount: 4},
  {id: 3, name: 'Need Progress',  description: 'Downloads in progress', gameCount: 2},
  {id: 4, name: 'Solo Pick',      description: 'Just the one, for now', gameCount: 1},
  {id: 5, name: 'Someday',        description: 'Nothing added yet',    gameCount: 0},
]

export const COLLECTION_GAMES = {
  1: [1, 6, 9, 13, 16, 20],
  2: [3, 8, 13, 16],
  3: [2, 15],
  4: [12],
  5: [],
}

export const DOWNLOAD_LINKS = [
  {id: 101, url: 'https://pixeldrain.com/u/abcd',  host: 'pixeldrain', name: 'Starforge v0.14.1.zip',   platform: 'windows', isDead: false},
  {id: 102, url: 'https://buzzheavier.com/f/efgh', host: 'buzzheavier', name: 'Starforge v0.14.1.zip', platform: 'linux',   isDead: false},
  {id: 103, url: 'https://workupload.com/start/ijkl', host: 'workupload', name: 'Starforge v0.14.1.zip', platform: 'windows', isDead: true},
  {id: 104, url: 'https://gofile.io/d/mnop',       host: 'gofile', name: 'Starforge v0.14.1.zip',     platform: 'windows', isDead: false},
]

export const DELETED_GAMES = [
  P(99, 'Old Project X', 'Unity', '0.9.0', '', 'abandoned', '2.1 GB', 2254857830),
]

export const DUPLICATES = [
  {
    title: 'Starforge',
    count: 2,
    games: [GAMES[1], {...GAMES[1], id: 21, path: '/mnt/games/archive/Starforge'}],
  },
  {
    title: 'Midnight Protocol',
    count: 2,
    games: [GAMES[3], {...GAMES[3], id: 24, title: 'Midnight Protocol [GOG copy]', path: '/mnt/games/archive/Midnight_Protocol'}],
  },
]

export const SEARCH_RESULTS = [
  {title: 'Midnight Protocol [v2.0.5]', url: 'https://f95zone.to/threads/midnight-protocol.101500/', prefix: "[Unity]", thumbnailUrl: '', matchScore: 98},
  {title: "Midnight Protocol [Ch. 5] [v2.0.0]", url: 'https://f95zone.to/threads/midnight-protocol.101499/', prefix: "[Unity]", thumbnailUrl: '', matchScore: 91},
  {title: 'Paper Crowns [v1.0]', url: 'https://f95zone.to/threads/paper-crowns.99991/', prefix: "[RPGM]", thumbnailUrl: '', matchScore: 74},
]

export const THREAD_PREVIEW = {
  title: 'Midnight Protocol [v2.0.5] [Final]',
  version: 'v2.0.5',
  engine: 'Unity',
  developer: 'Relay Nine Games',
  status: 'active',
  coverUrl: '',
  overview: 'Hack the grid, uncover the conspiracy. Midnight Protocol is a narrative-driven hacker thriller with branching dialogue and a reactive world. Latest build adds chapter 6, a new faction, and rebalanced netrunning.',
  threadId: 101500,
}

export const SCAN_PATHS = [
  '/home/mili/Games',
  '/mnt/games/archive',
]

export const PLAY_HISTORY = [
  {playedAt: '2026-09-02T21:14:00Z', platform: 'linux', durationS: 4200},
  {playedAt: '2026-08-30T19:40:00Z', platform: 'linux', durationS: 7050},
]

export const INSTALL_TARGETS = [
  {id: 1, title: 'Ember Falls', path: '/games/Ember_Falls'},
]

export const COOKIE_STATUS = 'available'
export const APP_VERSION = '0.4.0-alpha'
export const GAME_COUNT = GAMES.length
export const UPDATABLE_COUNT = 3
export const UPDATABLE_GAMES = [GAMES[1], GAMES[3], GAMES[8]]