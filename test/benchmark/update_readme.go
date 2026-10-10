//go:build ignore

// Command update_readme rewrites the Results section of README.md (between the
// bench:start and bench:end markers) from a saved benchmark output file. It does not run
// the benchmarks: `make bench-libs` writes results.txt, `make bench-libs-update` reads it.
//
//	go run update_readme.go            # results.txt -> README.md
//	go run update_readme.go -in x.txt
//	go run update_readme.go -dry       # print the section, leave README.md alone
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	startMarker = "<!-- bench:start -->"
	endMarker   = "<!-- bench:end -->"
)

// labels override the scenario name shown in the table.
var labels = map[string]string{"WideWhere": "WideWhere (12 predicates)"}

var lineRe = regexp.MustCompile(`^BenchmarkBuild/([^/]+)/(\S+?)(?:-\d+)?\s+\d+\s+([\d.]+) ns/op\s+([\d.]+) B/op\s+([\d.]+) allocs/op`)

type samples struct{ ns, bytes, allocs []float64 }

func main() {
	in := flag.String("in", "results.txt", "saved `go test -bench -benchmem` output")
	readme := flag.String("readme", "README.md", "README to update")
	dry := flag.Bool("dry", false, "print the section instead of writing it")
	flag.Parse()

	out, err := os.ReadFile(*in)
	if err != nil {
		fatal(fmt.Errorf("%w (run `make bench-libs` first)", err))
	}
	section, err := render(string(out))
	if err != nil {
		fatal(err)
	}
	if *dry {
		fmt.Print(section)
		return
	}
	if err := splice(*readme, section); err != nil {
		fatal(err)
	}
	fmt.Println("updated", *readme)
}

func render(out string) (string, error) {
	var (
		scenarios, libs []string
		data            = map[string]*samples{}
		cpu, goVer      string
	)
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "cpu: "); ok {
			cpu = strings.TrimSpace(v)
			continue
		}
		if v, ok := strings.CutPrefix(line, "go: "); ok {
			goVer = strings.TrimSpace(v)
			continue
		}
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		scn, lib := m[1], m[2]
		if !slices.Contains(scenarios, scn) {
			scenarios = append(scenarios, scn)
		}
		if !slices.Contains(libs, lib) {
			libs = append(libs, lib)
		}
		s := data[scn+"/"+lib]
		if s == nil {
			s = &samples{}
			data[scn+"/"+lib] = s
		}
		ns, _ := strconv.ParseFloat(m[3], 64)
		b, _ := strconv.ParseFloat(m[4], 64)
		a, _ := strconv.ParseFloat(m[5], 64)
		s.ns, s.bytes, s.allocs = append(s.ns, ns), append(s.bytes, b), append(s.allocs, a)
	}
	if len(scenarios) == 0 {
		return "", fmt.Errorf("no benchmark lines parsed from output:\n%s", out)
	}

	count := len(data[scenarios[0]+"/"+libs[0]].ns)
	var w strings.Builder
	fmt.Fprintf(&w, "%s\n", startMarker)
	fmt.Fprintf(&w, "%s, Go %s, median of %d runs (`make bench-libs`, then `make bench-libs-update`). Lower is\n", cpu, goVer, count)
	w.WriteString("better; the last column is time relative to quill.\n\n")
	w.WriteString("| Scenario | Library | ns/op | B/op | allocs/op | Time vs quill |\n")
	w.WriteString("| --- | --- | ---: | ---: | ---: | ---: |\n")
	for _, scn := range scenarios {
		base := median(data[scn+"/quill"].ns)
		for i, lib := range libs {
			s := data[scn+"/"+lib]
			if s == nil {
				continue
			}
			name := ""
			if i == 0 {
				if name = labels[scn]; name == "" {
					name = scn
				}
			}
			ns := median(s.ns)
			fmt.Fprintf(&w, "|%s| %s | %s | %s | %s | %.1fx |\n", pad(name), lib,
				comma(ns), comma(median(s.bytes)), comma(median(s.allocs)), ns/base)
		}
	}
	fmt.Fprintf(&w, "%s", endMarker)
	return w.String(), nil
}

func splice(path, section string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	i, j := strings.Index(s, startMarker), strings.Index(s, endMarker)
	if i < 0 || j < i {
		return fmt.Errorf("%s: %s / %s markers not found", path, startMarker, endMarker)
	}
	return os.WriteFile(path, []byte(s[:i]+section+s[j+len(endMarker):]), 0o644)
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// pad wraps a cell in single spaces, but leaves an empty cell as one space ("| |"),
// the compact table style markdownlint (MD060) expects.
func pad(s string) string {
	if s == "" {
		return " "
	}
	return " " + s + " "
}

// comma formats a rounded value with thousands separators.
func comma(f float64) string {
	s := strconv.FormatInt(int64(f+0.5), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
