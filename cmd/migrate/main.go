package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/campsite-booking/campsite-booking/internal/platform/postgres"
)

func main() {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dsnFlag := fs.String("dsn", "", "Postgres DSN (falls back to DATABASE_URL)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	dsn := *dsnFlag
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}

	if err := run(fs.Args(), dsn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run implements the up|down|version|force subcommands over a Migrator. It
// is the testable core of the CLI: main only parses flags and reports the
// exit code.
func run(args []string, dsn string) error {
	if dsn == "" {
		return errors.New("missing DSN: pass -dsn or set DATABASE_URL")
	}
	if len(args) == 0 {
		return errors.New("missing subcommand: expected up|down|version|force N")
	}

	mg, err := postgres.NewMigrator(dsn)
	if err != nil {
		return err
	}
	defer mg.Close()

	switch args[0] {
	case "up":
		return mg.Up()
	case "down":
		return mg.Down()
	case "version":
		version, dirty, err := mg.Version()
		if err != nil {
			return err
		}
		if dirty {
			fmt.Printf("%d (dirty)\n", version)
		} else {
			fmt.Println(version)
		}
		return nil
	case "force":
		if len(args) < 2 {
			return errors.New("force requires a version argument")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", args[1], err)
		}
		return mg.Force(v)
	default:
		return fmt.Errorf("unknown subcommand %q: expected up|down|version|force N", args[0])
	}
}
