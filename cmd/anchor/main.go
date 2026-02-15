package main

import (
	"os"

	"github.com/andrew-d/anchor/cli"
	"github.com/andrew-d/anchor/modules/example"
)

func main() {
	c := cli.New()
	c.RegisterModule(&example.Module{})
	os.Exit(c.Run(os.Args[1:]))
}
