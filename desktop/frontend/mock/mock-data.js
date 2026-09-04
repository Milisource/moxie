// ─────────────────────────────────────────────────────────────
// Mock fixture data for standalone-browser development/screenshots.
// Mirrors the JSON shapes from desktop/app.go (DesktopGameSummary,
// DesktopGameDetail, DesktopDownloadLink, DesktopDownloadLinkWithGame,
// F95SearchResult, ThreadPreview). Never shipped — dev-only entry point.
// ─────────────────────────────────────────────────────────────

const P = (id, title, engine, version, latestVersion, status, sizeLabel, sizeBytes, hasCover = true, developer = 'Pixel Bloom Studio', overview = '') => ({
  id, title, engine, version, latestVersion, status,
  path: `/games/${title.replace(/\W+/g, '_')}`,
  exePath: '',
  sizeBytes, sizeLabel,
  hasCover,
  developer,
  overview: overview || `${title} is a ${engine} game about choices, relationships, and the consequences that follow. The story branches based on the decisions you make, with multiple endings and a large cast of characters.`,
})

export const GAMES = [
  P(1,  'Ember Falls',       'Ren\'Py', '1.4.2', '1.4.2', 'active',    '2.6 GB', 2791728742),
  P(2,  'Starforge',         'Unity',   '0.12.0','0.14.1','active',    '9.1 GB', 9771050598),
  P(3,  'The Tides of Aurel', 'RPGM',   '3.2',   '3.4.1', 'completed', '1.9 GB', 2040109465),
  P(4,  'Midnight Protocol',  'Unity',   '2.0.5', '2.0.5', 'active',    '4.3 GB', 4617089843),
  P(5,  'Crimson Harvest',    'HTML',    '0.9.3', '1.0.0', 'on_hold',   '640 MB', 671088640),
  P(6,  'Glass Garden',       'Ren\'Py', '0.7.0', '0.7.0', 'active',    '1.2 GB', 1288490188),
  P(7,  'Voidwalker',         'Unity',   '0.4.1', '0.5.2', 'abandoned', '7.8 GB', 8375186227),
  P(8,  'Paper Crowns',       'RPGM',    '1.0',   '1.0',   'completed', '860 MB', 901775360),
  P(9,  'Solace',             'Ren\'Py', '2.1.0', '2.3.0', 'active',    '3.4 GB', 3650722201),
  P(10, 'Ironclad Hearts',    'UE4',     '0.6.2', '0.6.2', 'active',    '14.2 GB',15247133573),
  P(11, 'Whisperwood',        'HTML',    '0.3.0', '0.4.0', 'on_hold',   '220 MB', 230686720),
  P(12, 'Neon City Nights',   'Godot',   '1.2.3', '1.2.3', 'active',    '1.6 GB', 1717986918),
  P(13, 'The Alchemist\'s Daughter','Ren\'Py','5.0','5.0',   'completed', '5.7 GB', 6120328396),
  P(14, 'Frostfall',          'RPGM',    '0.8.0', '0.9.1', 'active',    '2.2 GB', 2362232012),
  P(15, 'Horizon Divide',     'Unity',   '1.0.0', '1.1.0', 'active',    '11.0 GB',11811160064),
  P(16, 'Starlit Sky',        'Ren\'Py', '4.2',   '4.2',   'completed', '4.0 GB', 4294967296),
  P(17, 'Dust & Echoes',      'WolfRPG', '1.1',   '1.1',   'unknown',   '380 MB', 398458880),
  P(18, 'Gilded Chains',      'Unity',   '0.10.0','0.11.0','active',    '6.2 GB', 6657199308),
  P(19, 'Running with Scissors','HTML',  '2.0',   '2.0',   'abandoned', '150 MB', 157286400),
  P(20, 'The Longest Night',  'Ren\'Py', '0.5.4', '0.5.4', 'active',    '2.9 GB', 3113851289),
]

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
}

export const COLLECTIONS = [
  {id: 1, name: 'Visual Novels',  description: 'Story-first games',   gameCount: 8},
  {id: 2, name: 'Finished',       description: 'Playable start to finish', gameCount: 5},
  {id: 3, name: 'Need Progress',  description: 'Downloads in progress', gameCount: 2},
]

export const COLLECTION_GAMES = {
  1: [1, 6, 9, 13, 16, 20],
  2: [3, 8, 13, 16],
  3: [2, 15],
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
  {threadId: 101500, title: 'Midnight Protocol [v2.0.5]', engine: 'Unity', version: 'v2.0.5', daysAgo: 2, views: '1.2M', rating: 4.2, category: 'Unity'},
  {threadId: 101499, title: 'Midnight Protocol [Ch. 5] [v2.0.0]', engine: 'Unity', version: 'v2.0.0', daysAgo: 12, views: '980K', rating: 4.1, category: 'Unity'},
  {threadId: 99991,  title: 'Paper Crowns [v1.0]', engine: 'RPGM', version: 'v1.0', daysAgo: 3, views: '45K', rating: 3.8, category: 'RPGM'},
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