package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected serve arguments")
		}
		tokens, err := postingTokens(*tokenFile)
		if err != nil {
			return err
		}
		s, err := openStore(*data)
		if err != nil {
			return err
		}
		defer s.db.Close()
		app := newApp(s, tokens)
		server := &http.Server{Addr: *listen, Handler: app.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		done := make(chan error, 1)
		go func() { log.Printf("slopchan listening on http://%s", *listen); done <- server.ListenAndServe() }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
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
