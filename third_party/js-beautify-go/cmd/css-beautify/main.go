// Command css-beautify formats CSS from files or stdin.
package main

import (
	"os"

	"github.com/yottonoko/js-beautify-go/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], "css-beautify", os.Stdin, os.Stdout, os.Stderr))
}
