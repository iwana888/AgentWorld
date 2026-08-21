// Command collect-feedback turns real developer answers to
// "what do you fear most when Claude/Codex writes code for you?"
// into a Policy backlog for the Reliability Runtime.
//
// Two modes:
//
//  1. Interactive (default): type one fear per line, blank line to finish.
//  2. File:        collect-feedback --answers answers.txt
//                  each line is one developer fear (free text or JSON
//                  {"text":"...","who":"dev@x"} ).
//
// Output: which of the 10 built-in policies each fear maps to (demand signal),
// plus a draft of NEW policies worth adding, with ready-to-paste Go skeletons.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"agent-reliability"
)

// keyword -> existing policy id. Used to map free-text fears to demand signal.
var fearMap = []struct {
	re   *regexp.Regexp
	pid  string
	note string
}{
	{regexp.MustCompile(`(?i)test|单元测试|改测试`), "NO_MODIFY_TESTS", "wants tests untouched"},
	{regexp.MustCompile(`(?i)delet|rm |del |删|清除|清掉|删除`), "NO_DELETE_FILES", "fears deletion"},
	{regexp.MustCompile(`(?i)reset --hard|重置|硬重置`), "NO_GIT_RESET_HARD", "fears git reset"},
	{regexp.MustCompile(`(?i)force push|--force|-f |强制推送|强推`), "NO_FORCE_PUSH", "fears force push"},
	{regexp.MustCompile(`(?i)\.env|secret|密钥|凭证|密码`), "NO_MODIFY_ENV", "fears secret edits"},
	{regexp.MustCompile(`(?i)read.*secret|读.*密钥|读.*凭证|vault`), "NO_READ_SECRETS", "fears secret reads"},
	{regexp.MustCompile(`(?i)drop table|truncate|delete from|删表|清表`), "NO_DROP_TABLE", "fears destructive SQL"},
	{regexp.MustCompile(`(?i)prod|生产|线上.*库`), "NO_PROD_DB", "fears prod DB"},
	{regexp.MustCompile(`(?i)deploy|发布|上线|部署`), "NO_PROD_DEPLOY", "fears prod deploy"},
	{regexp.MustCompile(`(?i)fork|while true|无限循环|递归.*爆`), "NO_FORK_BOMB", "fears runaway loops"},
}

type answer struct {
	Text string `json:"text"`
	Who  string `json:"who,omitempty"`
}

func main() {
	answersFile := flag.String("answers", "", "path to a file with one fear per line (text or JSON)")
	flag.Parse()

	var answers []answer
	if *answersFile != "" {
		answers = readFile(*answersFile)
	} else {
		answers = readInteractive()
	}
	if len(answers) == 0 {
		fmt.Println("no answers collected")
		return
	}

	// catalog of built-in policies for demand counting
	catalog := map[string]int{}
	for _, a := range answers {
		matched := false
		for _, m := range fearMap {
			if m.re.MatchString(a.Text) {
				catalog[m.pid]++
				matched = true
				fmt.Printf("  [map] %-18s <- %q\n", m.pid, truncate(a.Text, 60))
			}
		}
		if !matched {
			fmt.Printf("  [new] %q\n", truncate(a.Text, 60))
		}
	}

	report(catalog)
}

func readInteractive() []answer {
	fmt.Println("Paste each developer's fear (one per line). Blank line to finish:")
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []answer
	for {
		fmt.Print("> ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			break
		}
		out = append(out, answer{Text: line})
	}
	return out
}

func readFile(path string) []answer {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []answer
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// try JSON first
		var j answer
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &j) == nil && j.Text != "" {
			out = append(out, j)
			continue
		}
		out = append(out, answer{Text: line})
	}
	return out
}

func report(catalog map[string]int) {
	// verify built-in demand against the actual SDK catalog
	g := agentreliability.NewGuard()
	builtin := g.PolicyCatalog()

	fmt.Println("\n=== DEMAND SIGNAL (built-in policies) ===")
	valid := map[string]bool{}
	for _, id := range builtin {
		valid[id] = true
	}
	type row struct {
		id    string
		count int
	}
	rows := make([]row, 0, len(catalog))
	for id, c := range catalog {
		if valid[id] {
			rows = append(rows, row{id, c})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })
	for _, r := range rows {
		fmt.Printf("  %-18s %d mention(s)  [validated]\n", r.id, r.count)
	}

	fmt.Println("\n=== POLICY BACKLOG (what to add next) ===")
	if len(rows) == 0 {
		fmt.Println("  No clear demand yet — keep talking to developers.")
		fmt.Println("  Cheap kill criterion: if 10 devs give 0 mentions of any built-in")
		fmt.Println("  policy, the 'safe-by-construction' direction is not the pain point.")
		return
	}
	fmt.Println("  Top validated fears -> keep these policies ON by default.")
	fmt.Println("  Unmentioned built-in policies -> candidate to disable / drop.")
	fmt.Println("  Unmapped fears above -> write a new Policy (see skeleton):")
	fmt.Println()
	fmt.Println(`  rt.AddPolicy(agentreliability.Policy{
      ID:      "NO_<NAME>",
      Name:    "human-readable name",
      Severity:"high",           // low / medium / high / critical
      Enabled: true,
      Eval: func(a agentreliability.Action) (agentreliability.Decision, bool) {
          if a.Tool == "shell" && strings.Contains(a.Target, "<trigger>") {
              return agentreliability.Decision{
                  Type:     agentreliability.Deny, // or Ask / Modify
                  PolicyID: "NO_<NAME>",
                  Reason:   "why it is blocked",
              }, true
          }
          return agentreliability.Decision{}, false
      },
  })`)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
