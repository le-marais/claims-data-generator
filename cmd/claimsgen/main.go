// Command claimsgen generates fully synthetic insurance claims data -
// policies, claims and transactions, plus the monthly triangles and exposure
// aggregated from them - for use in reserving demos and tests.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
	"github.com/le-marais/claimsgen/internal/infrastructure/web"
)

const usage = `usage: claimsgen <command> [flags]

Commands:
  generate    generate policies.csv, claims.csv, transactions.csv,
              triangles.csv and exposure.csv
  ui          serve the browser UI on localhost

generate flags:
  --preset ID              embedded line of business: motor-personal or
                           motor-commercial (default motor-personal)
  --config PATH            line of business YAML, instead of a preset
  --seed N                 master random seed (default 1)
  --out DIR                output directory (default ./output)
  --start-year N           first calendar year of the book (default 1998)
  --years N                number of calendar years (default 10)
  --initial-book-size N    policies written in the first year, or fleets on a
                           fleet book (default 20000)
  --origin-basis B         monthly triangle origin: accident or underwriting (default accident)
  --section-detail         also write claim_sections.csv and
                           exposure_sections.csv, the section of cover of each
                           claim and the premium by origin month and section

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
	preset := fs.String("preset", "", "embedded line of business preset")
	configPath := fs.String("config", "", "line of business YAML file")
	seed := fs.Uint64("seed", 1, "master random seed")
	out := fs.String("out", "output", "output directory")
	startYear := fs.Int("start-year", 1998, "first calendar year")
	years := fs.Int("years", 10, "number of calendar years")
	initialBookSize := fs.Int("initial-book-size", 20000, "policies in the first year")
	originBasis := fs.String("origin-basis", "accident", "monthly triangle origin basis")
	sectionDetail := fs.Bool("section-detail", false, "also write claim_sections.csv and exposure_sections.csv")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	basis := triangle.OriginBasis(*originBasis)
	if err := basis.Validate(); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}

	if *preset != "" && *configPath != "" {
		fmt.Fprintln(stderr, "claimsgen: --preset and --config cannot be combined")
		return 2
	}
	var (
		l   lob.LineOfBusiness
		err error
	)
	switch {
	case *configPath != "":
		l, err = config.LoadFile(*configPath)
	case *preset != "":
		l, err = config.Preset(*preset)
	default:
		l, err = config.MotorPersonal()
	}
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: config: %v\n", err)
		return 1
	}

	ds, err := application.GenerateDataset(context.Background(), random.NewSource(*seed), application.GenerateRequest{
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

	ag, err := application.Aggregate(ds, *startYear, *years, basis)
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}
	if err := csvout.WriteAggregates(*out, ag); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}

	if *sectionDetail {
		names := make([]string, len(l.Claims.Sections))
		for i, sec := range l.Claims.Sections {
			names[i] = sec.Name
		}
		if err := csvout.WriteSectionDetail(*out, names, ds, ag); err != nil {
			fmt.Fprintf(stderr, "claimsgen: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "%s: wrote %d policies, %d claims, %d transactions, %d triangle rows, %d exposure rows to %s (seed %d)\n",
		l.Name, len(ds.Policies), len(ds.Claims), len(ds.Transactions),
		ag.Grid.Origins()*ag.Grid.DevPeriods, len(ag.Exposure), *out, *seed)
	return 0
}

func runUI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 8080, "port to listen on")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pools, err := schedulep.LoadPools(refdata.Files, refdata.LineFiles, application.ReferenceLines())
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: reference data: %v\n", err)
		return 1
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: cannot listen on port %d (%v); try --port\n", *port, err)
		return 1
	}
	fmt.Fprintf(stdout, "claimsgen ui: http://%s\n", ln.Addr())
	if err := http.Serve(ln, web.NewServer(pools)); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}
	return 0
}
