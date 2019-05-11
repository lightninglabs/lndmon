package main

import (
	"github.com/lightninglabs/lndmon"

	_ "github.com/lightninglabs/lndmon/collectors"
)

func main() {
	lndmon.Main()
}
