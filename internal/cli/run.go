package cli

import "github.com/alecthomas/kong"

func Run() {
	ctx := kong.Parse(&cli{})
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
