package main

import (
	"github.com/nsigel/deslop/internal/cli"

	// Register providers via init().
	_ "github.com/nsigel/deslop/internal/provider/amazon"
)

func main() {
	cli.Execute()
}
