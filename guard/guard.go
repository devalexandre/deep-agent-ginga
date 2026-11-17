// Package guard blocks commands that would change data in a database unless
// the user explicitly confirms that exact command.
//
// The check runs in code, before the tool executes; it does not depend on the
// model obeying its instructions. With no Confirmer installed (one-shot runs,
// editor servers, background runs) a flagged command is never executed.
//
// Limit: the guard reads the command the agent asks to run. It cannot see
// what a program does internally, so a script or binary that opens its own
// database connection is not detected. Give the agent read-only database
// credentials when that matters.
package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/devalexandre/agno-golang/agno/tools/toolkit"
)

// Request describes a command that needs the user's explicit confirmation.
type Request struct {
	Tool    string // toolkit name, e.g. "ShellTool"
	Command string // the full command the agent asked to run
	Reason  string // what in it changes data
}

// Confirmer asks the user about one command. It must return true only when
// the user answered yes to this Request; anything else keeps it blocked.
type Confirmer func(Request) bool

// maxScriptBytes caps how much of a referenced SQL file is read.
const maxScriptBytes = 4 << 20

var (
	sqlWrite = regexp.MustCompile(`(?is)\b(insert\s+(?:or\s+\w+\s+)?into|replace\s+into|merge\s+into|upsert\s+into|` +
		"update\\s+(?:only\\s+)?[\\w.\"`\\[\\]]+\\s+(?:as\\s+\\w+\\s+)?set\\b|" +
		`delete\s+from|truncate\s+(?:table\s+)?\w|` +
		`drop\s+(?:table|database|schema|index|view|column|materialized)|alter\s+table|copy\s+[\w."]+\s*(?:\([^)]*\)\s*)?from)`)

	shellOperators = regexp.MustCompile("[|;&<>`]|\\$\\(")

	// readOnly are commands that only show text. A bare call to one of them,
	// with no shell operator, is not a database change even when the text it
	// prints or searches contains SQL.
	readOnly = map[string]bool{
		"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true, "cat": true, "head": true,
		"tail": true, "less": true, "more": true, "wc": true, "ls": true, "find": true, "diff": true,
		"git": true, "file": true, "stat": true, "tree": true, "gofmt": true,
	}

	sqlClients = map[string]bool{
		"psql": true, "mysql": true, "mariadb": true, "sqlite3": true, "sqlite": true, "sqlcmd": true,
		"duckdb": true, "clickhouse-client": true, "clickhouse": true, "cockroach": true, "cqlsh": true,
		"usql": true, "pgcli": true, "mycli": true, "litecli": true, "bq": true, "snowsql": true,
	}

	// alwaysWrite are tools whose whole purpose is to change a database.
	alwaysWrite = map[string]bool{
		"pg_restore": true, "mysqlimport": true, "mongorestore": true, "mongoimport": true,
		"dropdb": true, "createdb": true, "dropuser": true,
	}

	scriptFlag = regexp.MustCompile(`(?i)(?:^|[\s"'])(?:-f|--file|-i|--init-command|-init|--init|\\i|\\ir|\.read|source)(?:\s+|=)("[^"]+"|'[^']+'|\S+)|<\s*("[^"]+"|'[^']+'|[^\s<&|;]+)`)

	mongoWrite = regexp.MustCompile(`(?i)\.(insert(?:One|Many)?|update(?:One|Many)?|delete(?:One|Many)?|replaceOne|remove|drop|dropDatabase|bulkWrite|findOneAnd(?:Update|Delete|Replace)|save|renameCollection|createIndex|dropIndex(?:es)?)\s*\(`)

	redisWrite = regexp.MustCompile(`(?i)\b(set|setex|setnx|mset|del|unlink|flushall|flushdb|hset|hmset|hdel|lpush|rpush|lpop|rpop|lrem|sadd|srem|zadd|zrem|expire|persist|rename|incr|incrby|decr|decrby|append|xadd|xdel|restore|migrate)\b`)

	migrations = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:migrate|golang-migrate)\b.*\b(up|down|drop|force|goto)\b`),
		regexp.MustCompile(`(?i)\bgoose\b.*\b(up|up-to|up-by-one|down|down-to|reset|redo)\b`),
		regexp.MustCompile(`(?i)\batlas\b\s+(?:migrate\s+apply|schema\s+apply|schema\s+clean)`),
		regexp.MustCompile(`(?i)\bdbmate\b.*\b(up|migrate|rollback|down|drop|load)\b`),
		regexp.MustCompile(`(?i)\bflyway\b.*\b(migrate|clean|undo|repair|baseline)\b`),
		regexp.MustCompile(`(?i)\bliquibase\b.*\b(update|rollback|drop-all|dropAll)`),
		regexp.MustCompile(`(?i)\bprisma\b\s+(?:migrate\s+(?:deploy|dev|reset|resolve)|db\s+(?:push|seed|execute))`),
		regexp.MustCompile(`(?i)\balembic\b.*\b(upgrade|downgrade|stamp)\b`),
		regexp.MustCompile(`(?i)\b(?:rails|rake)\b\s+db:\w+`),
		regexp.MustCompile(`(?i)\bmanage\.py\b\s+(migrate|flush|loaddata|sqlflush)\b`),
		regexp.MustCompile(`(?i)\bsqlx\b\s+(?:migrate\s+(?:run|revert)|database\s+(?:drop|reset|setup))`),
		regexp.MustCompile(`(?i)\bknex\b\s+(?:migrate:\w+|seed:run)`),
		regexp.MustCompile(`(?i)\bsequelize(?:-cli)?\b\s+db:\w+`),
		regexp.MustCompile(`(?i)\btypeorm\b\s+(?:migration:(?:run|revert)|schema:(?:sync|drop))`),
		regexp.MustCompile(`(?i)\btern\b\s+migrate\b`),
		regexp.MustCompile(`(?i)\bsql-migrate\b\s+(up|down|redo)\b`),
	}
)

// Inspect reports why command would change data in a database, or "" when
// nothing in it does. dir is where relative script paths are resolved.
func Inspect(command, dir string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	words := commandWords(command)
	if len(words) > 0 && readOnly[words[0]] && !shellOperators.MatchString(command) {
		return ""
	}
	if m := sqlWrite.FindString(command); m != "" {
		return "SQL that changes data: " + oneLine(m)
	}
	names := map[string]bool{}
	for _, w := range words {
		names[w] = true
	}
	for name := range names {
		if alwaysWrite[name] {
			return name + " changes the database"
		}
	}
	for _, re := range migrations {
		if m := re.FindString(command); m != "" {
			return "database migration: " + oneLine(m)
		}
	}
	if names["mongosh"] || names["mongo"] {
		if m := mongoWrite.FindString(command); m != "" {
			return "MongoDB write: " + oneLine(m)
		}
	}
	if names["redis-cli"] || names["valkey-cli"] {
		if m := redisWrite.FindString(command); m != "" {
			return "Redis write: " + oneLine(m)
		}
	}
	client := ""
	for name := range names {
		if sqlClients[name] || name == "mongosh" || name == "mongo" {
			client = name
		}
	}
	if client == "" {
		return ""
	}
	for _, m := range scriptFlag.FindAllStringSubmatch(command, -1) {
		path := strings.Trim(m[1]+m[2], `"'`)
		if path == "" || strings.HasPrefix(path, "-") {
			continue
		}
		if reason := inspectScript(path, dir, client); reason != "" {
			return reason
		}
	}
	// Um arquivo pode chegar ao cliente por outro caminho (cat x.sql | psql):
	// confere todo arquivo citado no comando.
	for _, w := range wordRE.FindAllString(command, -1) {
		path := w
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if reason := inspectScript(w, dir, client); reason != "" {
			return reason
		}
	}
	return ""
}

// inspectScript reads a script handed to a database client. A script that
// cannot be read is treated as a change: the guard cannot vouch for it.
func inspectScript(path, dir, client string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Sprintf("%s runs the file %s, which could not be read to check it", client, filepath.Base(path))
	}
	if info.Size() > maxScriptBytes {
		return fmt.Sprintf("%s runs the file %s, too large to check", client, filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%s runs the file %s, which could not be read to check it", client, filepath.Base(path))
	}
	if m := sqlWrite.Find(data); m != nil {
		return fmt.Sprintf("the file %s has SQL that changes data: %s", filepath.Base(path), oneLine(string(m)))
	}
	if m := mongoWrite.Find(data); m != nil && strings.HasPrefix(client, "mongo") {
		return fmt.Sprintf("the file %s has a MongoDB write: %s", filepath.Base(path), oneLine(string(m)))
	}
	return ""
}

var wordRE = regexp.MustCompile(`[^\s|;&<>()` + "`" + `"']+`)

// commandWords returns the program-like words of a command: each token with
// its directory and any env-assignment removed, lowercased.
func commandWords(command string) []string {
	var out []string
	for _, w := range wordRE.FindAllString(command, -1) {
		if strings.Contains(w, "=") && !strings.HasPrefix(w, "-") {
			continue
		}
		out = append(out, strings.ToLower(filepath.Base(w)))
	}
	return out
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// Gate holds the Confirmer shared by the guarded tools of one agent.
type Gate struct {
	mu      sync.RWMutex
	confirm Confirmer
	dir     string
}

// NewGate creates a gate that resolves script paths against dir. Without a
// Confirmer it blocks every flagged command.
func NewGate(dir string, confirm Confirmer) *Gate {
	return &Gate{dir: dir, confirm: confirm}
}

// SetConfirmer installs or removes (nil) the way to ask the user.
func (g *Gate) SetConfirmer(confirm Confirmer) {
	g.mu.Lock()
	g.confirm = confirm
	g.mu.Unlock()
}

// BlockedError is returned to the model when a command was not executed.
type BlockedError struct {
	Request Request
	Denied  bool // true: the user said no; false: nobody could be asked
}

func (e *BlockedError) Error() string {
	how := "the user cannot be asked for confirmation in this run mode"
	if e.Denied {
		how = "the user did not confirm it"
	}
	return fmt.Sprintf("BLOCKED: the command was not run (%s; %s). "+
		"Changing data in a database (INSERT, UPDATE, DELETE and the like) needs the user's explicit confirmation. "+
		"Do not retry through another command, script or tool: tell the user the exact command and wait.",
		e.Request.Reason, how)
}

// Check returns nil when the command may run: it changes no data, or the user
// confirmed it just now.
func (g *Gate) Check(tool, command string) error {
	reason := Inspect(command, g.dir)
	if reason == "" {
		return nil
	}
	req := Request{Tool: tool, Command: command, Reason: reason}
	g.mu.RLock()
	confirm := g.confirm
	g.mu.RUnlock()
	if confirm == nil {
		return &BlockedError{Request: req}
	}
	if !confirm(req) {
		return &BlockedError{Request: req, Denied: true}
	}
	return nil
}

// Wrap returns tool with every call checked by the gate first. The command is
// taken from all the text arguments of the call, so it covers "command",
// "args", "query", "sql" and whatever else the tool accepts.
func (g *Gate) Wrap(tool toolkit.Tool) toolkit.Tool {
	return &guarded{Tool: tool, gate: g}
}

type guarded struct {
	toolkit.Tool
	gate *Gate
}

func (t *guarded) Execute(methodName string, input json.RawMessage) (interface{}, error) {
	if err := t.gate.Check(t.GetName(), commandText(input)); err != nil {
		return nil, err
	}
	return t.Tool.Execute(methodName, input)
}

// commandText joins the text arguments of a tool call, "command" first.
func commandText(input json.RawMessage) string {
	var args map[string]interface{}
	if err := json.Unmarshal(input, &args); err != nil {
		return string(input)
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "command") != (keys[j] == "command") {
			return keys[i] == "command"
		}
		return keys[i] < keys[j]
	})
	var parts []string
	var add func(v interface{})
	add = func(v interface{}) {
		switch x := v.(type) {
		case string:
			parts = append(parts, x)
		case []interface{}:
			for _, e := range x {
				add(e)
			}
		case map[string]interface{}:
			for _, e := range x {
				add(e)
			}
		}
	}
	for _, k := range keys {
		if k == "working_dir" {
			continue
		}
		add(args[k])
	}
	return strings.Join(parts, " ")
}
