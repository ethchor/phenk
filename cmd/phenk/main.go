// Command phenk is the whole of Phenk: one binary, several run modes.
//
//	phenk smtpd    accept inbound mail
//	phenk api      serve the HTTP API and the inbox app
//	phenk worker   run parse and lifecycle jobs
//	phenk all      run all three in one process, for self-hosters
//	phenk migrate  apply pending migrations and exit
//	phenk genkey   print a new master key
//	phenk domain   manage the domain pools
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethchor/phenk/internal/config"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/pg"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	command, args := os.Args[1], os.Args[2:]

	// genkey and version deliberately need no configuration: an operator runs
	// genkey precisely because they do not have a master key yet.
	switch command {
	case "genkey":
		fmt.Println(crypto.GenerateMasterKey())
		return
	case "version", "-v", "--version":
		fmt.Println("phenk", version)
		return
	case "help", "-h", "--help":
		usage()
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, command, args); err != nil {
		if errors.Is(err, errUnknownCommand) {
			usage()
		}
		slog.Error("phenk failed", "command", command, "error", err)
		os.Exit(1)
	}
}

var errUnknownCommand = errors.New("unknown command")

func run(ctx context.Context, command string, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg)

	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := pg.Open(openCtx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return err
	}
	defer db.Close()

	if command == "migrate" {
		return db.Migrate(ctx)
	}
	if cfg.Database.Migrate {
		if err := db.Migrate(ctx); err != nil {
			return err
		}
	}

	if command == "domain" {
		return domainCommand(ctx, db, args)
	}

	rt, err := newRuntime(cfg, db)
	if err != nil {
		return err
	}

	switch command {
	case "smtpd":
		return rt.runSMTPD(ctx)
	case "api", "worker", "all":
		return notYetImplemented(command)
	default:
		return fmt.Errorf("%w: %s", errUnknownCommand, command)
	}
}

// notYetImplemented keeps the run modes visible in the CLI while the phases
// that fill them in land. It fails loudly rather than starting a process that
// silently does nothing.
func notYetImplemented(command string) error {
	phases := map[string]string{
		"worker": "phase 3",
		"api":    "phase 4",
		"all":    "phases 3 and 4",
	}
	return fmt.Errorf("phenk %s arrives in %s; the configuration and database it needs are already wired up", command, phases[command])
}

func domainCommand(ctx context.Context, db *pg.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: phenk domain <list|add|state> ...")
	}
	switch args[0] {
	case "list":
		return listDomains(ctx, db)
	case "add":
		if len(args) < 3 {
			return errors.New("usage: phenk domain add <name> <random|public> [fresh|active]")
		}
		state := core.DomainFresh
		if len(args) > 3 {
			state = core.DomainState(args[3])
		}
		d := &core.Domain{Name: strings.ToLower(args[1]), Pool: core.Pool(args[2]), State: state}
		if !d.Pool.Valid() {
			return fmt.Errorf("pool must be random or public, not %q", args[2])
		}
		if err := pg.CreateDomain(ctx, db, d); err != nil {
			return err
		}
		fmt.Printf("added %s (%s pool, %s)\n", d.Name, d.Pool, d.State)
		return nil
	case "state":
		if len(args) < 3 {
			return errors.New("usage: phenk domain state <name> <fresh|active|burned|retired>")
		}
		d, err := pg.DomainByName(ctx, db, strings.ToLower(args[1]))
		if err != nil {
			return err
		}
		if err := pg.SetDomainState(ctx, db, d.ID, core.DomainState(args[2])); err != nil {
			return err
		}
		fmt.Printf("%s is now %s\n", d.Name, args[2])
		return nil
	default:
		return fmt.Errorf("unknown domain subcommand %q", args[0])
	}
}

func listDomains(ctx context.Context, db *pg.DB) error {
	for _, pool := range []core.Pool{core.PoolRandom, core.PoolPublic} {
		domains, err := pg.AllocatableDomains(ctx, db, pool)
		if err != nil {
			return err
		}
		fmt.Printf("%s pool (%d active):\n", pool, len(domains))
		for _, d := range domains {
			fmt.Printf("  %s\n", d.Name)
		}
	}
	return nil
}

func setupLogging(cfg *config.Config) {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler = slog.NewJSONHandler(os.Stderr, opts)
	if cfg.Env == "development" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func usage() {
	fmt.Fprint(os.Stderr, `phenk — ephemeral email

usage: phenk <command> [arguments]

run modes:
  smtpd     accept inbound mail
  api       serve the HTTP API and the inbox app
  worker    run parse and lifecycle jobs
  all       run all three in one process

operations:
  migrate   apply pending migrations and exit
  genkey    print a new master key for PHENK_MASTER_KEY
  domain    list, add, or change the state of a domain
  version   print the version

configuration is read from the environment; PHENK_DATABASE_URL and
PHENK_MASTER_KEY are required.
`)
}
