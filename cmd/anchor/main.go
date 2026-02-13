package main

import (
	"os"

	"github.com/andrew-d/anchor/cli"
)

func main() {
	c := cli.New()
	os.Exit(c.Run(os.Args[1:]))
}
