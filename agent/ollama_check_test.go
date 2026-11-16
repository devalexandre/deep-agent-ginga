package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckOllamaModel(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       string // trecho do erro; vazio = sem erro
	}{
		{"ok", `{"capabilities":["tools","completion"],"parameters":"num_ctx 8192\ntemperature 0"}`, 200, ""},
		{"sem num_ctx", `{"capabilities":["completion","tools"],"parameters":"temperature 1"}`, 200, ""},
		{"sem tools", `{"capabilities":["completion"],"parameters":""}`, 200, "não aceita chamadas de ferramenta"},
		{"não existe", `{"error":"model not found"}`, 404, "ollama pull"},
		{"servidor sem /api/show", `nope`, 501, ""},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		_, err := checkOllamaModel(context.Background(), srv.URL, "", "m")
		srv.Close()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: erro inesperado: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: esperava erro com %q, veio %v", c.name, c.want, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"capabilities":["completion","tools","thinking"]}`))
	}))
	defer srv.Close()
	if thinking, err := checkOllamaModel(context.Background(), srv.URL, "", "m"); err != nil || !thinking {
		t.Errorf("modelo com raciocínio: thinking=%v err=%v", thinking, err)
	}
	if _, err := checkOllamaModel(context.Background(), "http://127.0.0.1:1", "", "m"); err == nil || !strings.Contains(err.Error(), "não respondeu") {
		t.Errorf("servidor fora do ar: veio %v", err)
	}
}
