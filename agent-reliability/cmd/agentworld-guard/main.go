// Command agentworld-guard is a tiny standalone CLI for the Agent Reliability Runtime.
//
// It reads one Action as JSON from stdin and prints the Decision as JSON to stdout.
// No AgentWorld dependency, no network — drop it in front of any agent's tool calls.
//
// Input:
//
//	{"tool":"shell","target":"rm -rf /"}
//
// Output:
//
//	{"decision":"DENY","policy":"NO_DELETE_FILES","reason":"..."}
//
// Flags:
//
//	--catalog   print the enabled policy IDs and exit
//	--pretty    indent the JSON output
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"agent-reliability"
)

// wireInput lets the user send either {"tool","target",...} or the friendlier
// {"tool","command",...} shape used in docs/examples.
type wireInput struct {
	Tool    string         `json:"tool"`
	Target  string         `json:"target"`
	Command string         `json:"command"`
	Args    map[string]any `json:"args"`
}

func main() {
	catalog := flag.Bool("catalog", false, "print enabled policy IDs and exit")
	pretty := flag.Bool("pretty", false, "indent JSON output")
	eval := flag.String("eval", "", "evaluate a single Action JSON directly (bypasses stdin)")
	evalFile := flag.String("eval-file", "", "read Action JSON from a file (no stdin/BOM issues)")
	flag.Parse()

	g := agentreliability.NewGuard()

	if *catalog {
		b, _ := json.Marshal(g.PolicyCatalog())
		fmt.Println(string(b))
		return
	}

	var raw []byte
	var err error
	switch {
	case *evalFile != "":
		raw, err = os.ReadFile(*evalFile)
		if err != nil {
			fail("read file: " + err.Error())
		}
	case *eval != "":
		raw = []byte(*eval)
	default:
		raw, err = io.ReadAll(os.Stdin)
		if err != nil {
			fail("read stdin: " + err.Error())
		}
	}

	var w wireInput
	if err := json.Unmarshal(raw, &w); err != nil {
		fail("parse action: " + err.Error())
	}
	target := w.Target
	if target == "" {
		target = w.Command // accept the docs-friendly "command" field
	}
	act := agentreliability.Action{Tool: w.Tool, Target: target, Args: w.Args}

	dec := g.Check(context.Background(), act)
	out, err := marshal(dec, *pretty)
	if err != nil {
		fail("marshal decision: " + err.Error())
	}
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
}

func marshal(v any, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(v, "", "  ")
	}
	return json.Marshal(v)
}

func fail(msg string) {
	os.Stderr.WriteString("agentworld-guard: " + msg + "\n")
	os.Exit(2)
}
