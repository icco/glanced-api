// Command glanced-api serves the Glanced card sync API.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.icco.me/gutil/logging"
	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if cmd != "serve" && cmd != "user" {
		return fmt.Errorf("unknown command %q (want serve or user)", cmd)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	g, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	if cmd == "user" {
		return userCreate(ctx, g, args, stdin, stdout)
	}
	return serve(ctx, g)
}

func serve(ctx context.Context, g *gorm.DB) error {
	log := logging.Must(logging.NewLogger("glanced-api"))
	defer logging.Sync(log)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: server.New(g, log.Desugar()), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Infow("listening", "port", port)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func userCreate(ctx context.Context, g *gorm.DB, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: glanced-api user create --email <email> < password")
	}
	fs := flag.NewFlagSet("user create", flag.ContinueOnError)
	email := fs.String("email", "", "email address for the new user")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("--email is required")
	}
	pw, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read password: %w", err)
	}
	u, err := auth.CreateUser(ctx, g, *email, strings.TrimRight(pw, "\r\n"))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, u.ID)
	return err
}
