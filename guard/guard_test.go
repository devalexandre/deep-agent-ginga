package guard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devalexandre/agno-golang/agno/tools/toolkit"
)

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "seed.sql"), []byte("-- seed\nINSERT INTO users(id) VALUES (1);\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "report.sql"), []byte("SELECT count(*) FROM users;\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "clean.js"), []byte("db.users.deleteMany({})\n"), 0o644)

	flagged := []string{
		`psql -c "DELETE FROM users WHERE id = 1"`,
		`psql -c "delete   from users"`,
		`psql "$DSN" -c 'UPDATE users SET name = $1'`,
		`mysql -e "insert into t values (1)"`,
		`sqlite3 app.db "INSERT OR REPLACE INTO t VALUES (1)"`,
		`sqlite3 app.db 'drop table t'`,
		`psql -c "TRUNCATE TABLE users"`,
		`psql -c "ALTER TABLE users ADD COLUMN x int"`,
		`echo "DELETE FROM users" | psql`,
		`cat seed.sql | psql mydb`,
		`psql -f seed.sql`,
		`psql --file=seed.sql`,
		`psql mydb < seed.sql`,
		`mysql mydb < missing.sql`,
		`sqlite3 app.db ".read seed.sql"`,
		`curl -d "UPDATE users SET a=1" http://localhost:8123`,
		`go run ./cmd/tool --sql "DELETE FROM x"`,
		`pg_restore -d mydb dump.tar`,
		`dropdb mydb`,
		`migrate -path db/migrations -database "$DSN" up`,
		`goose postgres "$DSN" up`,
		`atlas migrate apply --url "$DSN"`,
		`npx prisma migrate deploy`,
		`python manage.py migrate`,
		`rails db:migrate`,
		`mongosh --eval 'db.users.deleteMany({})'`,
		`mongosh mydb clean.js --file clean.js`,
		`redis-cli FLUSHALL`,
		`redis-cli set key value`,
		`/usr/bin/psql -c "COPY users FROM '/tmp/u.csv'"`,
		`PGPASSWORD=x psql -c "delete from t"`,
	}
	for _, c := range flagged {
		if Inspect(c, dir) == "" {
			t.Errorf("deveria pedir confirmação: %s", c)
		}
	}

	allowed := []string{
		`psql -c "SELECT * FROM users"`,
		`psql -f report.sql`,
		`sqlite3 app.db ".schema"`,
		`grep -rn "DELETE FROM" internal/`,
		`rg "UPDATE users SET"`,
		`cat seed.sql`,
		`git log --grep "insert into"`,
		`go test ./...`,
		`go build ./cmd/migrate`,
		`redis-cli GET key`,
		`mongosh --eval 'db.users.find({})'`,
		`pg_dump mydb`,
		`ls db/migrations`,
		`gofmt -l .`,
		`make test`,
	}
	for _, c := range allowed {
		if r := Inspect(c, dir); r != "" {
			t.Errorf("não deveria pedir confirmação: %s (%s)", c, r)
		}
	}
}

type fakeTool struct {
	toolkit.Toolkit
	ran int
}

func (f *fakeTool) Execute(string, json.RawMessage) (interface{}, error) {
	f.ran++
	return "ok", nil
}

func TestGate(t *testing.T) {
	inner := &fakeTool{Toolkit: toolkit.NewToolkit()}
	inner.Name = "ShellTool"
	gate := NewGate(t.TempDir(), nil)
	tool := gate.Wrap(inner)
	write := json.RawMessage(`{"command":"psql","args":["-c","DELETE FROM users"]}`)
	read := json.RawMessage(`{"command":"psql","args":["-c","SELECT 1"]}`)

	if _, err := tool.Execute("Execute", read); err != nil || inner.ran != 1 {
		t.Fatalf("leitura deveria rodar: err=%v ran=%d", err, inner.ran)
	}

	// Sem como perguntar: nunca executa.
	var blocked *BlockedError
	if _, err := tool.Execute("Execute", write); !errors.As(err, &blocked) || blocked.Denied || inner.ran != 1 {
		t.Fatalf("sem confirmador deveria bloquear: err=%v ran=%d", err, inner.ran)
	}

	// Usuário nega.
	var asked []Request
	gate.SetConfirmer(func(r Request) bool { asked = append(asked, r); return false })
	if _, err := tool.Execute("Execute", write); !errors.As(err, &blocked) || !blocked.Denied || inner.ran != 1 {
		t.Fatalf("negado deveria bloquear: err=%v ran=%d", err, inner.ran)
	}
	if len(asked) != 1 || asked[0].Command != "psql -c DELETE FROM users" {
		t.Fatalf("pedido errado: %+v", asked)
	}

	// Usuário confirma: roda, e a confirmação não vale para a próxima chamada.
	gate.SetConfirmer(func(r Request) bool { asked = append(asked, r); return true })
	if _, err := tool.Execute("Execute", write); err != nil || inner.ran != 2 {
		t.Fatalf("confirmado deveria rodar: err=%v ran=%d", err, inner.ran)
	}
	if _, err := tool.Execute("Execute", write); err != nil || len(asked) != 3 {
		t.Fatalf("cada alteração pede confirmação de novo: err=%v pedidos=%d", err, len(asked))
	}
	if _, err := tool.Execute("Execute", read); err != nil || len(asked) != 3 {
		t.Fatalf("leitura não pergunta: err=%v pedidos=%d", err, len(asked))
	}
}
