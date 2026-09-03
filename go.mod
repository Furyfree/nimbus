module github.com/Furyfree/nimbus

go 1.27.0

require (
	// Strict TOML decoding for nimbus.toml, definitions, and the selector.
	github.com/pelletier/go-toml/v2 v2.4.3
	// Command tree and flags for the CLI.
	github.com/spf13/cobra v1.10.2
)

require (
	// Pulled in by Cobra: its Windows-only double-click launch check. It
	// compiles to nothing on Linux.
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	// Pulled in by Cobra: its flag parser.
	github.com/spf13/pflag v1.0.9 // indirect
)
