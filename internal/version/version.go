// Package version reports the engine build identity and the schema ranges it
// supports.
package version

// Engine is the build version. Release builds override it with -ldflags.
var Engine = "0.0.0-dev"

// Schema ranges supported by this engine.
const (
	DefinitionSchema = 1
	OutputSchema     = 1
)

// Info is the structured version report.
type Info struct {
	Engine           string `json:"engine"`
	DefinitionSchema int    `json:"definition_schema"`
	OutputSchema     int    `json:"output_schema"`
}

// Current returns the report for the running engine.
func Current() Info {
	return Info{Engine: Engine, DefinitionSchema: DefinitionSchema, OutputSchema: OutputSchema}
}
