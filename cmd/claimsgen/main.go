// Command claimsgen generates fully synthetic insurance claims data -
// policies, claims and transactions - for use in reserving demos and tests.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
	"github.com/le-marais/claimsgen/internal/infrastructure/web"
)

const usage = `usage: claimsgen <command> [flags]

Commands:
  generate    generate policies.csv, claims.csv and transactions.csv
  ui          serve the browser UI on localhost

generate flags:
  --config PATH            line of business YAML (default: embedded motor-personal preset)
  --seed N                 master random seed (default 1)
  --out DIR                output directory (default ./output)
  --start-year N           first calendar year of the book (default 1998)
  --years N                number of calendar years (default 10)
  --initial-book-size N    policies written in the first year (default 20000)

ui flags:
  --port N                 port to listen on (default 8080)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "ui":
		return runUI(args[1:], stdout, stderr)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "line of business YAML file")
	seed := fs.Uint64("seed", 1, "master random seed")
	out := fs.String("out", "output", "output directory")
	startYear := fs.Int("start-year", 1998, "first calendar year")
	years := fs.Int("years", 10, "number of calendar years")
	initialBookSize := fs.Int("initial-book-size", 20000, "policies in the first year")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var (
		l   lob.LineOfBusiness
		err error
	)
	if *configPath == "" {
		l, err = config.MotorPersonal()
	} else {
		l, err = config.LoadFile(*configPath)
	}
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: config: %v\n", err)
		return 1
	}

	ds, err := application.GenerateDataset(random.NewSource(*seed), application.GenerateRequest{
		LOB:             l,
		StartYear:       *startYear,
		Years:           *years,
		InitialBookSize: *initialBookSize,
	})
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}

	if err := csvout.WriteDataset(*out, ds); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "%s: wrote %d policies, %d claims, %d transactions to %s (seed %d)\n",
		l.Name, len(ds.Policies), len(ds.Claims), len(ds.Transactions), *out, *seed)
	return 0
}

func runUI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 8080, "port to listen on")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	refs, err := schedulep.LoadFS(refdata.Files, "ppauto_pos98-07")
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: reference data: %v\n", err)
		return 1
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: cannot listen on port %d (%v); try --port\n", *port, err)
		return 1
	}
	fmt.Fprintf(stdout, "claimsgen ui: http://127.0.0.1:%d\n", *port)
	if err := http.Serve(ln, web.NewServer(refs)); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}
	return 0
}
