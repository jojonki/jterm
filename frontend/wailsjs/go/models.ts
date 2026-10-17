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
	export class SessionInfo {
	    id: string;
	    name: string;
	    host: string;
	    port: number;
	    user: string;
	    keyFile: string;
	    hasPassword: boolean;
	    lastUsed: number;
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
	        this.lastUsed = source["lastUsed"];
	        this.password = source["password"];
	        this.clearPassword = source["clearPassword"];
	    }
	}

}

