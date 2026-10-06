export namespace main {
	
	export class UpdateInfo {
	    current: string;
	    latest: string;
	    available: boolean;
	    canInstall: boolean;
	    notesUrl: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.canInstall = source["canInstall"];
	        this.notesUrl = source["notesUrl"];
	        this.error = source["error"];
	    }
	}

}

export namespace remote {
	
	export class State {
	    status: string;
	    host?: string;
	    http?: string;
	    token?: string;
	    message?: string;
	    installed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.host = source["host"];
	        this.http = source["http"];
	        this.token = source["token"];
	        this.message = source["message"];
	        this.installed = source["installed"];
	    }
	}

}

export namespace sshtunnel {
	
	export class Discovered {
	    alias?: string;
	    hostName: string;
	    user?: string;
	    port?: number;
	    keyPath?: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new Discovered(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.hostName = source["hostName"];
	        this.user = source["user"];
	        this.port = source["port"];
	        this.keyPath = source["keyPath"];
	        this.source = source["source"];
	    }
	}

}

