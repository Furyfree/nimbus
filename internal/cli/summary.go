package cli

import (
	"fmt"
	"io"
)

type runStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func renderRunSummary(out io.Writer, command string, steps []runStep) {
	fmt.Fprintf(out, "\n%s summary:\n", command)
	if len(steps) == 0 {
		fmt.Fprintln(out, "  unchanged: nothing needed to run")
	}
	for _, step := range steps {
		fmt.Fprintf(out, "  %-10s %s", step.Status, step.Name)
		if step.Detail != "" {
			fmt.Fprintf(out, ": %s", step.Detail)
		}
		fmt.Fprintln(out)
	}
}
