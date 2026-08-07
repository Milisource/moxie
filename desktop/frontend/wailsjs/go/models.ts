export namespace db {
	
	export class DownloadLink {
	    id: number;
	    game_id: number;
	    url: string;
	    host: string;
	    name: string;
	    platform: string;
	    is_dead: boolean;
	    dead_reason?: string;
	    // Go type: time
	    last_checked?: any;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new DownloadLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.game_id = source["game_id"];
	        this.url = source["url"];
	        this.host = source["host"];
	        this.name = source["name"];
	        this.platform = source["platform"];
	        this.is_dead = source["is_dead"];
	        this.dead_reason = source["dead_reason"];
	        this.last_checked = this.convertValues(source["last_checked"], null);
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class DependencyStatus {
	    name: string;
	    status: string;
	    details: string;
	
	    static createFrom(source: any = {}) {
	        return new DependencyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.details = source["details"];
	    }
	}
	export class DesktopCollection {
	    id: number;
	    name: string;
	    gameCount: number;

	    static createFrom(source: any = {}) {
	        return new DesktopCollection(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.gameCount = source["gameCount"];
	    }
	}
	export class DesktopDownloadLink {
	    id: number;
	    url: string;
	    host: string;
	    name: string;
	    platform: string;
	    isDead: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DesktopDownloadLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.host = source["host"];
	        this.name = source["name"];
	        this.platform = source["platform"];
	        this.isDead = source["isDead"];
	    }
	}
	export class DesktopDownloadLinkWithGame {
	    id: number;
	    url: string;
	    host: string;
	    name: string;
	    platform: string;
	    isDead: boolean;
	    gameId: number;
	    gameTitle: string;
	    gamePath: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopDownloadLinkWithGame(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.host = source["host"];
	        this.name = source["name"];
	        this.platform = source["platform"];
	        this.isDead = source["isDead"];
	        this.gameId = source["gameId"];
	        this.gameTitle = source["gameTitle"];
	        this.gamePath = source["gamePath"];
	    }
	}
	export class DesktopPlayEntry {
	    playedAt: string;
	    platform: string;
	    durationS: number;
	
	    static createFrom(source: any = {}) {
	        return new DesktopPlayEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.playedAt = source["playedAt"];
	        this.platform = source["platform"];
	        this.durationS = source["durationS"];
	    }
	}
	export class DesktopGameDetail {
	    id: number;
	    title: string;
	    engine: string;
	    version: string;
	    latestVersion: string;
	    status: string;
	    path: string;
	    exePath: string;
	    sizeBytes: number;
	    sizeLabel: string;
	    hasCover: boolean;
	    developer: string;
	    overview: string;
	    coverUrl: string;
	    f95Url: string;
	    tags: string[];
	    notes: string;
	    storeLinks: Record<string, string>;
	    steamAppId: number;
	    winePrefix: string;
	    downloadLinks: DesktopDownloadLink[];
	    playHistory: DesktopPlayEntry[];
	
	    static createFrom(source: any = {}) {
	        return new DesktopGameDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.engine = source["engine"];
	        this.version = source["version"];
	        this.latestVersion = source["latestVersion"];
	        this.status = source["status"];
	        this.path = source["path"];
	        this.exePath = source["exePath"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sizeLabel = source["sizeLabel"];
	        this.hasCover = source["hasCover"];
	        this.developer = source["developer"];
	        this.overview = source["overview"];
	        this.coverUrl = source["coverUrl"];
	        this.f95Url = source["f95Url"];
	        this.tags = source["tags"];
	        this.notes = source["notes"];
	        this.storeLinks = source["storeLinks"];
	        this.steamAppId = source["steamAppId"];
	        this.winePrefix = source["winePrefix"];
	        this.downloadLinks = this.convertValues(source["downloadLinks"], DesktopDownloadLink);
	        this.playHistory = this.convertValues(source["playHistory"], DesktopPlayEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DesktopGameSummary {
	    id: number;
	    title: string;
	    engine: string;
	    version: string;
	    latestVersion: string;
	    status: string;
	    path: string;
	    exePath: string;
	    sizeBytes: number;
	    sizeLabel: string;
	    hasCover: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DesktopGameSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.engine = source["engine"];
	        this.version = source["version"];
	        this.latestVersion = source["latestVersion"];
	        this.status = source["status"];
	        this.path = source["path"];
	        this.exePath = source["exePath"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sizeLabel = source["sizeLabel"];
	        this.hasCover = source["hasCover"];
	    }
	}
	
	export class DetectionResult {
	    engine: string;
	    version: string;
	    title: string;
	    sizeBytes: number;
	    sizeLabel: string;
	    path: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DetectionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = source["engine"];
	        this.version = source["version"];
	        this.title = source["title"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sizeLabel = source["sizeLabel"];
	        this.path = source["path"];
	        this.error = source["error"];
	    }
	}
	export class DuplicateGroup {
	    title: string;
	    count: number;
	    games: DesktopGameSummary[];
	
	    static createFrom(source: any = {}) {
	        return new DuplicateGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.count = source["count"];
	        this.games = this.convertValues(source["games"], DesktopGameSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EditGameFields {
	    engine: string;
	    version: string;
	    exePath: string;
	    notes: string;
	
	    static createFrom(source: any = {}) {
	        return new EditGameFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = source["engine"];
	        this.version = source["version"];
	        this.exePath = source["exePath"];
	        this.notes = source["notes"];
	    }
	}
	export class F95DownloadLink {
	    url: string;
	    name: string;
	    host: string;
	    platform: string;
	
	    static createFrom(source: any = {}) {
	        return new F95DownloadLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.platform = source["platform"];
	    }
	}
	export class F95SearchResult {
	    title: string;
	    url: string;
	    prefix: string;
	    thumbnailUrl: string;
	    matchScore: number;
	
	    static createFrom(source: any = {}) {
	        return new F95SearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.url = source["url"];
	        this.prefix = source["prefix"];
	        this.thumbnailUrl = source["thumbnailUrl"];
	        this.matchScore = source["matchScore"];
	    }
	}
	export class StatusCount {
	    status: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new StatusCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.count = source["count"];
	    }
	}
	export class ThreadPreview {
	    title: string;
	    developer: string;
	    version: string;
	    overview: string;
	    coverUrl: string;
	    tags: string[];
	    status: string;
	    storeLinks: Record<string, string>;
	    downloadLinks: F95DownloadLink[];
	    prefix: string;
	
	    static createFrom(source: any = {}) {
	        return new ThreadPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.developer = source["developer"];
	        this.version = source["version"];
	        this.overview = source["overview"];
	        this.coverUrl = source["coverUrl"];
	        this.tags = source["tags"];
	        this.status = source["status"];
	        this.storeLinks = source["storeLinks"];
	        this.downloadLinks = this.convertValues(source["downloadLinks"], F95DownloadLink);
	        this.prefix = source["prefix"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UpdateInfo {
	    hasUpdate: boolean;
	    currentVersion: string;
	    latestVersion: string;
	    releaseUrl: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasUpdate = source["hasUpdate"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseUrl = source["releaseUrl"];
	        this.error = source["error"];
	    }
	}

}

