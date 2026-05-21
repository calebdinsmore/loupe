// Command loupe starts a local code-review web app for the current git repo.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/calebjdinsmore/loupe/internal/agent"
	"github.com/calebjdinsmore/loupe/internal/git"
	"github.com/calebjdinsmore/loupe/internal/prompts"
	"github.com/calebjdinsmore/loupe/internal/server"
	"github.com/calebjdinsmore/loupe/internal/store"
)

func main() {
	root, err := gitRoot()
	if err != nil {
		log.Fatalf("loupe must be run inside a git repository: %v", err)
	}

	dataDir := filepath.Join(root, ".loupe")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}

	if err := prompts.New(root).Seed(); err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(filepath.Join(dataDir, "loupe.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	srv := server.New(git.New(root), st, agent.New(root), prompts.New(root))

	// LOUPE_ADDR pins the port for `vite dev` proxying; otherwise pick any free one.
	addr := os.Getenv("LOUPE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	url := fmt.Sprintf("http://%s", ln.Addr().String())
	log.Printf("loupe serving %s  (repo: %s)", url, root)
	openBrowser(url)

	httpSrv := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func gitRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
}
