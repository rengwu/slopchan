package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

// A file keeps service credentials out of command lines and service definitions.
func postingTokens(tokenFile string) ([]string, error) {
	value := os.Getenv("SLOPCHAN_TOKENS")
	if tokenFile != "" {
		if value != "" {
			return nil, errors.New("configure only one of SLOPCHAN_TOKENS and a token file")
		}
		contents, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("read posting token file: %w", err)
		}
		value = strings.TrimSpace(string(contents))
	}
	var tokens []string
	for _, token := range strings.Split(value, ",") {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("set SLOPCHAN_TOKENS or SLOPCHAN_TOKEN_FILE to one or more comma-separated bearer tokens")
	}
	return tokens, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "version":
		fmt.Println("slopchan " + version)
		return nil
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		data := flags.String("data", env("SLOPCHAN_DATA_DIR", "./data"), "persistent data directory")
		listen := flags.String("listen", env("SLOPCHAN_LISTEN", "127.0.0.1:8080"), "HTTP listen address")
		tokenFile := flags.String("token-file", env("SLOPCHAN_TOKEN_FILE", ""), "file containing comma-separated posting tokens")
		adminEmail := flags.String("admin-email", env("SLOPCHAN_ADMIN_EMAIL", ""), "initial admin email")
		adminPassword := flags.String("admin-password", "", "initial admin password (prefer environment or password file)")
		adminPasswordFile := flags.String("admin-password-file", env("SLOPCHAN_ADMIN_PASSWORD_FILE", ""), "file containing initial admin password")
		adminReset := flags.Bool("reset-admin", false, "replace saved admin credentials with supplied credentials and log out all sessions")
		trustProxy := flags.Bool("trust-proxy", env("SLOPCHAN_TRUST_PROXY", "false") == "true", "trust X-Forwarded-Proto from an HTTPS proxy; restrict direct access to the backend")
		allowInsecureAdmin := flags.Bool("allow-insecure-admin", env("SLOPCHAN_ALLOW_INSECURE_ADMIN", "false") == "true", "allow admin access over unencrypted HTTP (default: require HTTPS)")
		tlsCert := flags.String("tls-cert", env("SLOPCHAN_TLS_CERT", ""), "TLS certificate file")
		tlsKey := flags.String("tls-key", env("SLOPCHAN_TLS_KEY", ""), "TLS private key file")
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected serve arguments")
		}
		// Never use a password as a flag default: flag help prints default values.
		passwordFlagSet := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "admin-password" {
				passwordFlagSet = true
			}
		})
		if !passwordFlagSet {
			*adminPassword = os.Getenv("SLOPCHAN_ADMIN_PASSWORD")
		}

		if (*tlsCert == "") != (*tlsKey == "") {
			return errors.New("configure both TLS certificate and key")
		}
		if *adminPasswordFile != "" {
			if *adminPassword != "" {
				return errors.New("configure only one admin password source")
			}
			value, err := os.ReadFile(*adminPasswordFile)
			if err != nil {
				return fmt.Errorf("read admin password file: %w", err)
			}
			*adminPassword = strings.TrimRight(string(value), "\r\n")
		}
		if *adminEmail != "" || *adminPassword != "" || *adminReset {
			if _, err := validateCredentials(*adminEmail, *adminPassword); err != nil {
				return err
			}
		}
		var tokens []string
		if *tokenFile != "" || os.Getenv("SLOPCHAN_TOKENS") != "" {
			var err error
			tokens, err = postingTokens(*tokenFile)
			if err != nil {
				return err
			}
		} else if *adminEmail == "" {
			if _, err := os.Stat(filepath.Join(*data, "slopchan.db")); err != nil {
				return errors.New("configure admin email and password, or posting tokens")
			}
		}
		log.Printf("Starting slopchan %s...", version)
		s, err := openStore(*data)
		if err != nil {
			return err
		}
		defer s.db.Close()
		if err = s.bootstrapAdmin(context.Background(), *adminEmail, *adminPassword, *adminReset); err != nil {
			return err
		}
		var credentials int
		if err = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM admin)+(SELECT COUNT(*) FROM access_tokens WHERE revoked_at='')`).Scan(&credentials); err != nil {
			return err
		}
		if credentials == 0 && len(tokens) == 0 {
			return errors.New("configure admin email and password, or posting tokens")
		}
		app, err := newApp(s, tokens)
		if err != nil {
			return err
		}
		app.trustProxy = *trustProxy
		app.allowInsecureAdmin = *allowInsecureAdmin
		server := &http.Server{Addr: *listen, Handler: app.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		defer listener.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		done := make(chan error, 1)
		go func() {
			if *tlsCert != "" {
				done <- server.ServeTLS(listener, *tlsCert, *tlsKey)
			} else {
				done <- server.Serve(listener)
			}
		}()
		scheme := "http"
		if *tlsCert != "" {
			scheme = "https"
		}
		log.Printf("Listening on %s://%s", scheme, listener.Addr())
		log.Printf("Board data: %s", s.dir)
		log.Print("Server is running. Open the address above in your browser. Press Ctrl+C to stop.")
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			log.Print("Stopping slopchan...")
			shutdown, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				return err
			}
			log.Print("slopchan stopped.")
			return nil
		}
	case "remove":
		flags := flag.NewFlagSet("remove", flag.ContinueOnError)
		data := flags.String("data", env("SLOPCHAN_DATA_DIR", "./data"), "persistent data directory")
		imageOnly := flags.Bool("image-only", false, "remove only the image, retaining the text")
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("usage: slopchan remove [-data directory] [-image-only] POST_ID")
		}
		id, err := strconv.ParseInt(flags.Arg(0), 10, 64)
		if err != nil || id < 1 {
			return errors.New("post ID must be a positive integer")
		}
		s, err := openStore(*data)
		if err != nil {
			return err
		}
		defer s.db.Close()
		if err = s.remove(context.Background(), id, *imageOnly); err != nil {
			return err
		}
		fmt.Printf("Removed content from post #%d. Its permalink is preserved.\n", id)
		return nil
	default:
		return fmt.Errorf("unknown command %q; use serve, remove, or version", command)
	}
}
