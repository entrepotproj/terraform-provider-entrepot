// Package acctest runs provider acceptance tests against a real Entrepot.
//
// Either point the tests at a running server:
//
//	ENTREPOT_URL=http://localhost:8080 ENTREPOT_TOKEN=<admin token>
//
// or let the harness start one from a binary, with a fresh database:
//
//	ENTREPOT_BINARY=/path/to/entrepot \
//	ENTREPOT_TEST_DATABASE_URL=postgres://postgres@localhost:5432/postgres?sslmode=disable
//
// Acceptance tests also need TF_ACC=1 and a terraform (or tofu) binary
// (TF_ACC_TERRAFORM_PATH).
package acctest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

// Server describes the Entrepot instance under test.
type Server struct {
	URL   string
	Token string
	API   *client.API
	// DataDir is the directory file blob stores may use (harness-started
	// servers only; empty otherwise).
	DataDir string

	stop func()
}

var current *Server

// GitLabInstance is the name of the trusted GitLab instance configured on
// harness-started servers (for GitLab trust policy tests). Servers given
// with ENTREPOT_URL must configure an instance with this name under
// auth.gitlab, or set ENTREPOT_TEST_GITLAB to the name they use.
var GitLabInstance = func() string {
	if v := os.Getenv("ENTREPOT_TEST_GITLAB"); v != "" {
		return v
	}
	return "gitlab"
}()

// Main starts the server (if needed) around the test run. Use it from
// TestMain: func TestMain(m *testing.M) { acctest.Main(m) }.
func Main(m *testing.M) {
	code := func() int {
		if os.Getenv("TF_ACC") == "" {
			return m.Run() // unit tests only; acceptance tests skip themselves
		}
		s, err := start()
		if err != nil {
			fmt.Fprintln(os.Stderr, "acctest:", err)
			return 1
		}
		current = s
		defer s.stop()
		return m.Run()
	}()
	os.Exit(code)
}

// Get returns the server, or skips the test when acceptance tests are off.
func Get(t *testing.T) *Server {
	t.Helper()
	if os.Getenv("TF_ACC") == "" || current == nil {
		t.Skip("set TF_ACC=1 (and ENTREPOT_URL/ENTREPOT_TOKEN or ENTREPOT_BINARY) to run acceptance tests")
	}
	return current
}

// ProviderBlock returns HCL configuring the provider for this server.
func (s *Server) ProviderBlock() string {
	return fmt.Sprintf("provider \"entrepot\" {\n  url   = %q\n  token = %q\n}\n", s.URL, s.Token)
}

// RandName returns a unique object name with the given prefix.
func RandName(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func start() (*Server, error) {
	if u := os.Getenv("ENTREPOT_URL"); u != "" {
		tok := os.Getenv("ENTREPOT_TOKEN")
		api, err := client.New(client.Options{URL: u, Token: tok})
		if err != nil {
			return nil, err
		}
		return &Server{URL: strings.TrimRight(u, "/"), Token: tok, API: api, DataDir: os.Getenv("ENTREPOT_DATA_DIR"), stop: func() {}}, nil
	}
	bin := os.Getenv("ENTREPOT_BINARY")
	if bin == "" {
		return nil, fmt.Errorf("TF_ACC is set but neither ENTREPOT_URL nor ENTREPOT_BINARY is")
	}
	admin := os.Getenv("ENTREPOT_TEST_DATABASE_URL")
	if admin == "" {
		admin = "postgres://postgres@localhost:5432/postgres?sslmode=disable"
	}
	ctx := context.Background()
	dbName := "entrepot_tfacc_" + strings.ReplaceAll(RandName("x")[2:], "-", "")
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", admin, err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		conn.Close(ctx)
		return nil, err
	}
	conn.Close(ctx)
	dbURL, _ := url.Parse(admin)
	dbURL.Path = "/" + dbName

	dir, err := os.MkdirTemp("", "entrepot-tfacc-")
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(dir, "data")
	_ = os.MkdirAll(dataDir, 0o750)
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	token := "tfacc-bootstrap-" + RandName("t")
	cfg := fmt.Sprintf(`server: {addr: "127.0.0.1:%d", baseURL: %q}
database: {url: %q}
auth:
  sessionSecret: "tfacc-session-secret-0123456789abcdef"
  bootstrapToken: %q
  # A trusted GitLab instance so GitLab trust policies can be managed
  # (OIDC discovery is lazy, so the issuer is never contacted).
  gitlab: [{name: %q, issuerURL: "https://gitlab.example.com", audience: %q}]
storage: {allowedRoots: [%q], tempDir: %q}
scan: {workers: 0}
cleanup: {interval: 0s}
log: {format: text, level: warn}
`, port, base, dbURL.String(), token, GitLabInstance, base, dataDir, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return nil, err
	}
	logf, _ := os.Create(filepath.Join(dir, "server.log"))
	cmd := exec.Command(bin, "serve", "-config", cfgPath)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		logf.Close()
		if c, err := pgx.Connect(context.Background(), admin); err == nil {
			_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
			c.Close(context.Background())
		}
		if os.Getenv("ENTREPOT_KEEP") == "" {
			_ = os.RemoveAll(dir)
		} else {
			fmt.Fprintln(os.Stderr, "acctest: kept", dir)
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(base + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			stop()
			log, _ := os.ReadFile(filepath.Join(dir, "server.log"))
			return nil, fmt.Errorf("entrepot did not become ready: %s", log)
		}
		time.Sleep(200 * time.Millisecond)
	}
	api, err := client.New(client.Options{URL: base, Token: token})
	if err != nil {
		stop()
		return nil, err
	}
	return &Server{URL: base, Token: token, API: api, DataDir: dataDir, stop: stop}, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
