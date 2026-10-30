export namespace agentsession {
	
	export class Attachment {
	    path: string;
	    text?: string;
	    startLine?: number;
	    endLine?: number;
	
	    static createFrom(source: any = {}) {
	        return new Attachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.text = source["text"];
	        this.startLine = source["startLine"];
	        this.endLine = source["endLine"];
	    }
	}
	export class Policy {
	    autoApproveEdits: boolean;
	    autoApproveReads: boolean;
	    autoApproveAll: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Policy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoApproveEdits = source["autoApproveEdits"];
	        this.autoApproveReads = source["autoApproveReads"];
	        this.autoApproveAll = source["autoApproveAll"];
	    }
	}
	export class SessionInfo {
	    id: string;
	    agent: string;
	    model?: string;
	    cwd: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.agent = source["agent"];
	        this.model = source["model"];
	        this.cwd = source["cwd"];
	    }
	}

}

export namespace dap {
	
	export class Breakpoint {
	    id?: number;
	    verified: boolean;
	    line?: number;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new Breakpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.verified = source["verified"];
	        this.line = source["line"];
	        this.message = source["message"];
	    }
	}
	export class EvaluateResult {
	    result: string;
	    type?: string;
	    variablesReference: number;
	
	    static createFrom(source: any = {}) {
	        return new EvaluateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.result = source["result"];
	        this.type = source["type"];
	        this.variablesReference = source["variablesReference"];
	    }
	}
	export class Scope {
	    name: string;
	    variablesReference: number;
	    expensive?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Scope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.variablesReference = source["variablesReference"];
	        this.expensive = source["expensive"];
	    }
	}
	export class Source {
	    name?: string;
	    path?: string;
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class SourceBreakpoint {
	    line: number;
	    condition?: string;
	    hitCondition?: string;
	    logMessage?: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceBreakpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.condition = source["condition"];
	        this.hitCondition = source["hitCondition"];
	        this.logMessage = source["logMessage"];
	    }
	}
	export class StackFrame {
	    id: number;
	    name: string;
	    source?: Source;
	    line: number;
	    column: number;
	
	    static createFrom(source: any = {}) {
	        return new StackFrame(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.source = this.convertValues(source["source"], Source);
	        this.line = source["line"];
	        this.column = source["column"];
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
	export class Thread {
	    id: number;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Thread(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class Variable {
	    name: string;
	    value: string;
	    type?: string;
	    evaluateName?: string;
	    variablesReference: number;
	
	    static createFrom(source: any = {}) {
	        return new Variable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.type = source["type"];
	        this.evaluateName = source["evaluateName"];
	        this.variablesReference = source["variablesReference"];
	    }
	}

}

export namespace db {
	
	export class Column {
	    name: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	    }
	}
	export class Connection {
	    id: string;
	    name: string;
	    driver: string;
	    host: string;
	    port: number;
	    database: string;
	    user: string;
	    protocol: string;
	    tls: boolean;
	    readOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.driver = source["driver"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.database = source["database"];
	        this.user = source["user"];
	        this.protocol = source["protocol"];
	        this.tls = source["tls"];
	        this.readOnly = source["readOnly"];
	    }
	}
	export class DatabaseInfo {
	    name: string;
	    canConnect: boolean;
	    default: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DatabaseInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.canConnect = source["canConnect"];
	        this.default = source["default"];
	    }
	}
	export class RowEdit {
	    op: string;
	    key: Record<string, any>;
	    values: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new RowEdit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.key = source["key"];
	        this.values = source["values"];
	    }
	}
	export class EditBatch {
	    tableOid: number;
	    edits: RowEdit[];
	
	    static createFrom(source: any = {}) {
	        return new EditBatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tableOid = source["tableOid"];
	        this.edits = this.convertValues(source["edits"], RowEdit);
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
	export class EditColumn {
	    attnum: number;
	    name: string;
	    type: string;
	    kind: string;
	    notNull: boolean;
	    hasDefault: boolean;
	    primaryKey: boolean;
	    editable: boolean;
	    readOnlyReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new EditColumn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attnum = source["attnum"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.kind = source["kind"];
	        this.notNull = source["notNull"];
	        this.hasDefault = source["hasDefault"];
	        this.primaryKey = source["primaryKey"];
	        this.editable = source["editable"];
	        this.readOnlyReason = source["readOnlyReason"];
	    }
	}
	export class EditResult {
	    statements: number;
	    affected: number;
	    sql: string;
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new EditResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statements = source["statements"];
	        this.affected = source["affected"];
	        this.sql = source["sql"];
	        this.elapsedMs = source["elapsedMs"];
	    }
	}
	export class EditTarget {
	    tableOid: number;
	    schema: string;
	    table: string;
	    key: string[];
	    columns: EditColumn[];
	
	    static createFrom(source: any = {}) {
	        return new EditTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tableOid = source["tableOid"];
	        this.schema = source["schema"];
	        this.table = source["table"];
	        this.key = source["key"];
	        this.columns = this.convertValues(source["columns"], EditColumn);
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
	export class ExplainBuffers {
	    sharedHit: number;
	    sharedRead: number;
	    sharedDirtied: number;
	    sharedWritten: number;
	    localHit: number;
	    localRead: number;
	    tempRead: number;
	    tempWritten: number;
	
	    static createFrom(source: any = {}) {
	        return new ExplainBuffers(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sharedHit = source["sharedHit"];
	        this.sharedRead = source["sharedRead"];
	        this.sharedDirtied = source["sharedDirtied"];
	        this.sharedWritten = source["sharedWritten"];
	        this.localHit = source["localHit"];
	        this.localRead = source["localRead"];
	        this.tempRead = source["tempRead"];
	        this.tempWritten = source["tempWritten"];
	    }
	}
	export class ExplainLine {
	    depth: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new ExplainLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.depth = source["depth"];
	        this.text = source["text"];
	    }
	}
	export class PlanDetail {
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new PlanDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class PlanNode {
	    nodeType: string;
	    relationship?: string;
	    relation?: string;
	    alias?: string;
	    index?: string;
	    joinType?: string;
	    startupCost: number;
	    totalCost: number;
	    selfCost: number;
	    planRows: number;
	    planWidth: number;
	    analyzed: boolean;
	    neverExecuted?: boolean;
	    actualRows: number;
	    actualLoops: number;
	    actualStartupMs: number;
	    actualTotalMs: number;
	    totalMs: number;
	    selfMs: number;
	    filter?: string;
	    rowsRemovedByFilter?: number;
	    buffers?: ExplainBuffers;
	    details: PlanDetail[];
	    warnings: string[];
	    children: PlanNode[];
	
	    static createFrom(source: any = {}) {
	        return new PlanNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeType = source["nodeType"];
	        this.relationship = source["relationship"];
	        this.relation = source["relation"];
	        this.alias = source["alias"];
	        this.index = source["index"];
	        this.joinType = source["joinType"];
	        this.startupCost = source["startupCost"];
	        this.totalCost = source["totalCost"];
	        this.selfCost = source["selfCost"];
	        this.planRows = source["planRows"];
	        this.planWidth = source["planWidth"];
	        this.analyzed = source["analyzed"];
	        this.neverExecuted = source["neverExecuted"];
	        this.actualRows = source["actualRows"];
	        this.actualLoops = source["actualLoops"];
	        this.actualStartupMs = source["actualStartupMs"];
	        this.actualTotalMs = source["actualTotalMs"];
	        this.totalMs = source["totalMs"];
	        this.selfMs = source["selfMs"];
	        this.filter = source["filter"];
	        this.rowsRemovedByFilter = source["rowsRemovedByFilter"];
	        this.buffers = this.convertValues(source["buffers"], ExplainBuffers);
	        this.details = this.convertValues(source["details"], PlanDetail);
	        this.warnings = source["warnings"];
	        this.children = this.convertValues(source["children"], PlanNode);
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
	export class ExplainResult {
	    driver: string;
	    analyze: boolean;
	    statement: string;
	    raw: string;
	    plan?: PlanNode;
	    planningMs: number;
	    executionMs: number;
	    warnings: number;
	    rolledBack: boolean;
	    lines?: ExplainLine[];
	    needsConfirm: boolean;
	    confirmReason?: string;
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new ExplainResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.analyze = source["analyze"];
	        this.statement = source["statement"];
	        this.raw = source["raw"];
	        this.plan = this.convertValues(source["plan"], PlanNode);
	        this.planningMs = source["planningMs"];
	        this.executionMs = source["executionMs"];
	        this.warnings = source["warnings"];
	        this.rolledBack = source["rolledBack"];
	        this.lines = this.convertValues(source["lines"], ExplainLine);
	        this.needsConfirm = source["needsConfirm"];
	        this.confirmReason = source["confirmReason"];
	        this.elapsedMs = source["elapsedMs"];
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
	export class HistoryEntry {
	    id: number;
	    database: string;
	    sql: string;
	    at: number;
	    durationMs: number;
	    rows: number;
	    error?: string;
	    kind?: string;
	    truncated?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.database = source["database"];
	        this.sql = source["sql"];
	        this.at = source["at"];
	        this.durationMs = source["durationMs"];
	        this.rows = source["rows"];
	        this.error = source["error"];
	        this.kind = source["kind"];
	        this.truncated = source["truncated"];
	    }
	}
	export class HistoryFilter {
	    database: string;
	    allDatabases: boolean;
	    filter: string;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database = source["database"];
	        this.allDatabases = source["allDatabases"];
	        this.filter = source["filter"];
	        this.limit = source["limit"];
	    }
	}
	
	
	export class QueryResult {
	    columns: Column[];
	    rows: any[][];
	    truncated: boolean;
	    affected: number;
	    command: string;
	    elapsedMs: number;
	    tableOid?: number;
	    attnums?: number[];
	    editBlocked?: string;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = this.convertValues(source["columns"], Column);
	        this.rows = source["rows"];
	        this.truncated = source["truncated"];
	        this.affected = source["affected"];
	        this.command = source["command"];
	        this.elapsedMs = source["elapsedMs"];
	        this.tableOid = source["tableOid"];
	        this.attnums = source["attnums"];
	        this.editBlocked = source["editBlocked"];
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
	
	export class Table {
	    schema: string;
	    name: string;
	    kind: string;
	    columns: Column[];
	
	    static createFrom(source: any = {}) {
	        return new Table(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.columns = this.convertValues(source["columns"], Column);
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

export namespace fuzzy {
	
	export class Match {
	    path: string;
	    indices: number[];
	    root?: string;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new Match(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.indices = source["indices"];
	        this.root = source["root"];
	        this.offset = source["offset"];
	    }
	}
	export class Result {
	    total: number;
	    matches: Match[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.matches = this.convertValues(source["matches"], Match);
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

export namespace git {
	
	export class BlameRange {
	    line: number;
	    count: number;
	    commit: number;
	
	    static createFrom(source: any = {}) {
	        return new BlameRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.count = source["count"];
	        this.commit = source["commit"];
	    }
	}
	export class BlameCommit {
	    hash: string;
	    author: string;
	    email: string;
	    time: number;
	    summary: string;
	    path: string;
	    previous: string;
	    prevPath: string;
	    uncommitted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BlameCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.time = source["time"];
	        this.summary = source["summary"];
	        this.path = source["path"];
	        this.previous = source["previous"];
	        this.prevPath = source["prevPath"];
	        this.uncommitted = source["uncommitted"];
	    }
	}
	export class Blame {
	    root: string;
	    untracked: boolean;
	    commits: BlameCommit[];
	    ranges: BlameRange[];
	
	    static createFrom(source: any = {}) {
	        return new Blame(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.untracked = source["untracked"];
	        this.commits = this.convertValues(source["commits"], BlameCommit);
	        this.ranges = this.convertValues(source["ranges"], BlameRange);
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
	
	
	export class Branch {
	    name: string;
	    remote: boolean;
	    current: boolean;
	    upstream: string;
	    track: string;
	    updatedAt: number;
	    worktree: string;
	
	    static createFrom(source: any = {}) {
	        return new Branch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.remote = source["remote"];
	        this.current = source["current"];
	        this.upstream = source["upstream"];
	        this.track = source["track"];
	        this.updatedAt = source["updatedAt"];
	        this.worktree = source["worktree"];
	    }
	}
	export class Commit {
	    hash: string;
	    parents: string[];
	    author: string;
	    email: string;
	    time: number;
	    refs: string;
	    subject: string;
	
	    static createFrom(source: any = {}) {
	        return new Commit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.parents = source["parents"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.time = source["time"];
	        this.refs = source["refs"];
	        this.subject = source["subject"];
	    }
	}
	export class FileChange {
	    path: string;
	    origPath: string;
	    kind: string;
	    index: string;
	    worktree: string;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.origPath = source["origPath"];
	        this.kind = source["kind"];
	        this.index = source["index"];
	        this.worktree = source["worktree"];
	    }
	}
	export class FileCommit {
	    commit: Commit;
	    path: string;
	    origPath: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new FileCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.commit = this.convertValues(source["commit"], Commit);
	        this.path = source["path"];
	        this.origPath = source["origPath"];
	        this.kind = source["kind"];
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
	export class LogOptions {
	    limit: number;
	    skip: number;
	    all: boolean;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new LogOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.limit = source["limit"];
	        this.skip = source["skip"];
	        this.all = source["all"];
	        this.path = source["path"];
	    }
	}
	export class Revision {
	    exists: boolean;
	    binary: boolean;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new Revision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.binary = source["binary"];
	        this.content = source["content"];
	    }
	}
	export class Stash {
	    hash: string;
	    index: number;
	    message: string;
	    time: number;
	    untracked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Stash(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.index = source["index"];
	        this.message = source["message"];
	        this.time = source["time"];
	        this.untracked = source["untracked"];
	    }
	}
	export class Status {
	    branch: string;
	    head: string;
	    upstream: string;
	    ahead: number;
	    behind: number;
	    files: FileChange[];
	    operation: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.head = source["head"];
	        this.upstream = source["upstream"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.files = this.convertValues(source["files"], FileChange);
	        this.operation = source["operation"];
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
	export class Worktree {
	    path: string;
	    head: string;
	    branch: string;
	    main: boolean;
	    current: boolean;
	    detached: boolean;
	    locked: boolean;
	    prunable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Worktree(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.head = source["head"];
	        this.branch = source["branch"];
	        this.main = source["main"];
	        this.current = source["current"];
	        this.detached = source["detached"];
	        this.locked = source["locked"];
	        this.prunable = source["prunable"];
	    }
	}

}

export namespace httpfile {
	
	export class Error {
	    line: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Error(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.message = source["message"];
	    }
	}
	export class Header {
	    name: string;
	    value: string;
	    line?: number;
	
	    static createFrom(source: any = {}) {
	        return new Header(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.line = source["line"];
	    }
	}
	export class Mark {
	    line: number;
	    name: string;
	    method: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Mark(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	    }
	}
	export class Outline {
	    requests: Mark[];
	    errors: Error[];
	
	    static createFrom(source: any = {}) {
	        return new Outline(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requests = this.convertValues(source["requests"], Mark);
	        this.errors = this.convertValues(source["errors"], Error);
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
	export class Response {
	    id: string;
	    file: string;
	    key: string;
	    name: string;
	    method: string;
	    url: string;
	    finalUrl: string;
	    status: number;
	    statusText: string;
	    proto: string;
	    headers: Header[];
	    timeMs: number;
	    size: number;
	    contentType: string;
	    kind: string;
	    body: string;
	    truncated: boolean;
	    incomplete: boolean;
	    at: number;
	
	    static createFrom(source: any = {}) {
	        return new Response(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.file = source["file"];
	        this.key = source["key"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.finalUrl = source["finalUrl"];
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.proto = source["proto"];
	        this.headers = this.convertValues(source["headers"], Header);
	        this.timeMs = source["timeMs"];
	        this.size = source["size"];
	        this.contentType = source["contentType"];
	        this.kind = source["kind"];
	        this.body = source["body"];
	        this.truncated = source["truncated"];
	        this.incomplete = source["incomplete"];
	        this.at = source["at"];
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
	export class Target {
	    path: string;
	    text: string;
	    line: number;
	    env: string;
	
	    static createFrom(source: any = {}) {
	        return new Target(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.text = source["text"];
	        this.line = source["line"];
	        this.env = source["env"];
	    }
	}

}

export namespace lsp {
	
	export class Position {
	    line: number;
	    character: number;
	
	    static createFrom(source: any = {}) {
	        return new Position(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.character = source["character"];
	    }
	}
	export class Range {
	    start: Position;
	    end: Position;
	
	    static createFrom(source: any = {}) {
	        return new Range(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = this.convertValues(source["start"], Position);
	        this.end = this.convertValues(source["end"], Position);
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
	export class TextEdit {
	    range: Range;
	    newText: string;
	
	    static createFrom(source: any = {}) {
	        return new TextEdit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.range = this.convertValues(source["range"], Range);
	        this.newText = source["newText"];
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
	export class CompletionItem {
	    label: string;
	    kind: number;
	    detail: string;
	    documentation: string;
	    insertText: string;
	    filterText: string;
	    sortText: string;
	    edit?: TextEdit;
	
	    static createFrom(source: any = {}) {
	        return new CompletionItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.detail = source["detail"];
	        this.documentation = source["documentation"];
	        this.insertText = source["insertText"];
	        this.filterText = source["filterText"];
	        this.sortText = source["sortText"];
	        this.edit = this.convertValues(source["edit"], TextEdit);
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
	export class CompletionList {
	    incomplete: boolean;
	    items: CompletionItem[];
	
	    static createFrom(source: any = {}) {
	        return new CompletionList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.incomplete = source["incomplete"];
	        this.items = this.convertValues(source["items"], CompletionItem);
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
	export class Location {
	    path: string;
	    range: Range;
	    preview: string;
	
	    static createFrom(source: any = {}) {
	        return new Location(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.range = this.convertValues(source["range"], Range);
	        this.preview = source["preview"];
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
	
	
	export class Status {
	    language: string;
	    root: string;
	    state: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.root = source["root"];
	        this.state = source["state"];
	        this.message = source["message"];
	    }
	}

}

export namespace main {
	
	export class AgentInfo {
	    available: string[];
	    preferred: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.preferred = source["preferred"];
	    }
	}
	export class Collected {
	    dir: string;
	    path: string;
	    seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new Collected(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.path = source["path"];
	        this.seconds = source["seconds"];
	    }
	}
	export class FileHistory {
	    root: string;
	    commits: git.FileCommit[];
	
	    static createFrom(source: any = {}) {
	        return new FileHistory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.commits = this.convertValues(source["commits"], git.FileCommit);
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
	export class ProfiledTests {
	    title: string;
	    dir: string;
	    files: testrun.Profiles;
	
	    static createFrom(source: any = {}) {
	        return new ProfiledTests(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.dir = source["dir"];
	        this.files = this.convertValues(source["files"], testrun.Profiles);
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
	export class RepoInfo {
	    isRepo: boolean;
	    root: string;
	    gitDir: string;
	    commonDir: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isRepo = source["isRepo"];
	        this.root = source["root"];
	        this.gitDir = source["gitDir"];
	        this.commonDir = source["commonDir"];
	    }
	}
	export class RunConfigs {
	    saved: runconfig.Config[];
	    detected: runconfig.Config[];
	    capabilities: Record<string, runconfig.Capabilities>;
	    error: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new RunConfigs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = this.convertValues(source["saved"], runconfig.Config);
	        this.detected = this.convertValues(source["detected"], runconfig.Config);
	        this.capabilities = this.convertValues(source["capabilities"], runconfig.Capabilities, true);
	        this.error = source["error"];
	        this.path = source["path"];
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
	export class SendOptions {
	    followRedirects: boolean;
	    timeoutMs: number;
	
	    static createFrom(source: any = {}) {
	        return new SendOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.followRedirects = source["followRedirects"];
	        this.timeoutMs = source["timeoutMs"];
	    }
	}
	export class TitlebarMetrics {
	    height: number;
	    buttonsLeft: number;
	    buttonsTop: number;
	    buttonWidth: number;
	    buttonHeight: number;
	
	    static createFrom(source: any = {}) {
	        return new TitlebarMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.height = source["height"];
	        this.buttonsLeft = source["buttonsLeft"];
	        this.buttonsTop = source["buttonsTop"];
	        this.buttonWidth = source["buttonWidth"];
	        this.buttonHeight = source["buttonHeight"];
	    }
	}
	export class UndoResult {
	    restored: number;
	    skipped: string[];
	
	    static createFrom(source: any = {}) {
	        return new UndoResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.restored = source["restored"];
	        this.skipped = source["skipped"];
	    }
	}

}

export namespace preview {
	
	export class Image {
	    dataUrl: string;
	    mime: string;
	    size: number;
	    tooLarge: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Image(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dataUrl = source["dataUrl"];
	        this.mime = source["mime"];
	        this.size = source["size"];
	        this.tooLarge = source["tooLarge"];
	    }
	}

}

export namespace profiling {
	
	export class Frame {
	    name: string;
	    file?: string;
	    line?: number;
	    self: number;
	    total: number;
	    children?: Frame[];
	
	    static createFrom(source: any = {}) {
	        return new Frame(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.file = source["file"];
	        this.line = source["line"];
	        this.self = source["self"];
	        this.total = source["total"];
	        this.children = this.convertValues(source["children"], Frame);
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
	export class TopEntry {
	    name: string;
	    file?: string;
	    line?: number;
	    flat: number;
	    cum: number;
	
	    static createFrom(source: any = {}) {
	        return new TopEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.file = source["file"];
	        this.line = source["line"];
	        this.flat = source["flat"];
	        this.cum = source["cum"];
	    }
	}
	export class Report {
	    path: string;
	    sampleTypes: string[];
	    sampleType: string;
	    unit: string;
	    total: number;
	    durationNs: number;
	    root: Frame;
	    top: TopEntry[];
	    frames: number;
	    trimmed: number;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sampleTypes = source["sampleTypes"];
	        this.sampleType = source["sampleType"];
	        this.unit = source["unit"];
	        this.total = source["total"];
	        this.durationNs = source["durationNs"];
	        this.root = this.convertValues(source["root"], Frame);
	        this.top = this.convertValues(source["top"], TopEntry);
	        this.frames = source["frames"];
	        this.trimmed = source["trimmed"];
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

export namespace runconfig {
	
	export class Capabilities {
	    run: boolean;
	    debug: boolean;
	    profile: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Capabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run = source["run"];
	        this.debug = source["debug"];
	        this.profile = source["profile"];
	    }
	}
	export class Debug {
	    adapter?: string;
	    stopOnEntry?: boolean;
	    args?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Debug(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.adapter = source["adapter"];
	        this.stopOnEntry = source["stopOnEntry"];
	        this.args = source["args"];
	    }
	}
	export class Config {
	    id: string;
	    name: string;
	    type: string;
	    cwd?: string;
	    env?: Record<string, string>;
	    before?: string;
	    command?: string;
	    args?: string[];
	    package?: string;
	    buildFlags?: string[];
	    script?: string;
	    target?: string;
	    debug?: Debug;
	    profileURL?: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.cwd = source["cwd"];
	        this.env = source["env"];
	        this.before = source["before"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.package = source["package"];
	        this.buildFlags = source["buildFlags"];
	        this.script = source["script"];
	        this.target = source["target"];
	        this.debug = this.convertValues(source["debug"], Debug);
	        this.profileURL = source["profileURL"];
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

export namespace scratch {
	
	export class Scratch {
	    id: string;
	    name: string;
	    kind: string;
	    connId: string;
	    database: string;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Scratch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.connId = source["connId"];
	        this.database = source["database"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace search {
	
	export class Options {
	    root: string;
	    query: string;
	    regex: boolean;
	    caseSensitive: boolean;
	    wholeWord: boolean;
	    include: string;
	    exclude: string;
	    maxResults: number;
	    replaceOn: boolean;
	    replace: string;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.query = source["query"];
	        this.regex = source["regex"];
	        this.caseSensitive = source["caseSensitive"];
	        this.wholeWord = source["wholeWord"];
	        this.include = source["include"];
	        this.exclude = source["exclude"];
	        this.maxResults = source["maxResults"];
	        this.replaceOn = source["replaceOn"];
	        this.replace = source["replace"];
	    }
	}
	export class SkippedFile {
	    root: string;
	    path: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new SkippedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.path = source["path"];
	        this.reason = source["reason"];
	    }
	}
	export class ReplaceResult {
	    files: number;
	    replacements: number;
	    skipped: SkippedFile[];
	    undoable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ReplaceResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = source["files"];
	        this.replacements = source["replacements"];
	        this.skipped = this.convertValues(source["skipped"], SkippedFile);
	        this.undoable = source["undoable"];
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
	export class ReplaceTarget {
	    root: string;
	    path: string;
	    lines: number[];
	
	    static createFrom(source: any = {}) {
	        return new ReplaceTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.path = source["path"];
	        this.lines = source["lines"];
	    }
	}

}

export namespace testrun {
	
	export class Mark {
	    line: number;
	    kind: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Mark(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	    }
	}
	export class Profiles {
	    cpu: string;
	    mem?: string;
	
	    static createFrom(source: any = {}) {
	        return new Profiles(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cpu = source["cpu"];
	        this.mem = source["mem"];
	    }
	}
	export class Request {
	    lang: string;
	    dirs: string[];
	    file?: string;
	    tests?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lang = source["lang"];
	        this.dirs = source["dirs"];
	        this.file = source["file"];
	        this.tests = source["tests"];
	    }
	}

}

export namespace textdiff {
	
	export class Range {
	    fromA: number;
	    toA: number;
	    fromB: number;
	    toB: number;
	
	    static createFrom(source: any = {}) {
	        return new Range(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fromA = source["fromA"];
	        this.toA = source["toA"];
	        this.fromB = source["fromB"];
	        this.toB = source["toB"];
	    }
	}
	export class Hunk {
	    fromA: number;
	    toA: number;
	    fromB: number;
	    toB: number;
	    inline?: Range[];
	
	    static createFrom(source: any = {}) {
	        return new Hunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fromA = source["fromA"];
	        this.toA = source["toA"];
	        this.fromB = source["fromB"];
	        this.toB = source["toB"];
	        this.inline = this.convertValues(source["inline"], Range);
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
	export class LineMark {
	    line: number;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new LineMark(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.kind = source["kind"];
	    }
	}

}

export namespace workspace {
	
	export class Entry {
	    name: string;
	    path: string;
	    isDir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	    }
	}
	export class FileContent {
	    path: string;
	    content: string;
	    binary: boolean;
	    tooLarge: boolean;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new FileContent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	        this.binary = source["binary"];
	        this.tooLarge = source["tooLarge"];
	        this.size = source["size"];
	    }
	}

}

