export namespace main {
	
	export class ImportResult {
	    added: number;
	    skipped: number;
	    unsupported: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.skipped = source["skipped"];
	        this.unsupported = source["unsupported"];
	    }
	}
	export class Look {
	    theme?: string;
	    accent?: string;
	    background?: string;
	    image?: string;
	    imageOpacity?: number;
	
	    static createFrom(source: any = {}) {
	        return new Look(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.accent = source["accent"];
	        this.background = source["background"];
	        this.image = source["image"];
	        this.imageOpacity = source["imageOpacity"];
	    }
	}
	export class SessionInfo {
	    id: string;
	    name: string;
	    host: string;
	    port: number;
	    user: string;
	    keyFile: string;
	    hasPassword: boolean;
	    local: boolean;
	    shell: string;
	    lastUsed: number;
	    look?: Look;
	    password: string;
	    clearPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.keyFile = source["keyFile"];
	        this.hasPassword = source["hasPassword"];
	        this.local = source["local"];
	        this.shell = source["shell"];
	        this.lastUsed = source["lastUsed"];
	        this.look = this.convertValues(source["look"], Look);
	        this.password = source["password"];
	        this.clearPassword = source["clearPassword"];
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
	    available: boolean;
	    current: string;
	    latest: string;
	    notes: string;
	    pageUrl: string;
	    assetName: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.notes = source["notes"];
	        this.pageUrl = source["pageUrl"];
	        this.assetName = source["assetName"];
	    }
	}

}

