package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/7samael7/Piper/engine/internal/support"
)

func main() {
	write := flag.Bool("write", false, "write the generated provider support document")
	check := flag.Bool("check", false, "fail when the generated document is stale")
	output := flag.String("output", filepath.Join("..", "docs", "provider-support.md"), "output document path")
	flag.Parse()

	if *write == *check {
		fmt.Fprintln(os.Stderr, "choose exactly one of -write or -check")
		os.Exit(2)
	}
	registry, err := support.Default()
	if err != nil {
		fatal(err)
	}
	generated := support.RenderProviderSupport(registry)
	if *write {
		if err := os.WriteFile(*output, generated, 0o644); err != nil {
			fatal(err)
		}
		return
	}
	existing, err := os.ReadFile(*output)
	if err != nil {
		fatal(err)
	}
	if !bytes.Equal(existing, generated) {
		fatal(fmt.Errorf("%s is stale; run `cd engine && go run ./cmd/supportdoc -write`", *output))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
