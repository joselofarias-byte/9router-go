package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"9router/proxy/internal/measurement/freecoding"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "offline":
		offlineCmd(os.Args[2:])
	case "discover":
		discoverCmd(os.Args[2:])
	case "infer":
		inferCmd(os.Args[2:])
	case "report":
		reportCmd(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  free-coding-measure offline  -out DIR
  free-coding-measure discover -out DIR
  free-coding-measure infer    -out DIR -allow FILE provider/model-id ...
  free-coding-measure report   -jsonl FILE [-out FILE]

Default measurement is offline (fixtures only, no network).
Public discovery reads OpenRouter and Cline catalogs without credentials.
Authenticated inference runs only when FREE_CODING_INFERENCE=1 and a free
credential is set (OPENROUTER_FREE_API_KEY, CLINE_FREE_API_KEY, or
FREE_CODING_API_KEY). OPENROUTER_API_KEY is ignored.

Tiers stay separate. A catalog list is not a coding ranking.
`)
}

func offlineCmd(args []string) {
	fs := flag.NewFlagSet("offline", flag.ExitOnError)
	out := fs.String("out", "", "output directory")
	_ = fs.Parse(args)
	if *out == "" {
		fmt.Fprintln(os.Stderr, "-out is required")
		os.Exit(2)
	}
	recs, err := freecoding.OfflineRecords(context.Background(), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "offline: %v\n", err)
		os.Exit(1)
	}
	finish(*out, recs, false)
}

func discoverCmd(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	out := fs.String("out", "", "output directory")
	_ = fs.Parse(args)
	if *out == "" {
		fmt.Fprintln(os.Stderr, "-out is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	recs, err := freecoding.DiscoverPublic(ctx, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover: %v\n", err)
		os.Exit(1)
	}
	finish(*out, recs, true)
}

func inferCmd(args []string) {
	fs := flag.NewFlagSet("infer", flag.ExitOnError)
	out := fs.String("out", "", "output directory")
	allowPath := fs.String("allow", "", "jsonl from offline or discover")
	endpoint := fs.String("endpoint", "", "provider chat URL; default is the provider's own HTTPS endpoint")
	run := fs.Int("run", 1, "repetition number stored on each task run")
	_ = fs.Parse(args)
	if *out == "" || *run < 1 {
		fmt.Fprintln(os.Stderr, "-out is required and -run must be >= 1")
		os.Exit(2)
	}
	now := time.Now().UTC()
	models := fs.Args()
	if os.Getenv("FREE_CODING_INFERENCE") != "1" {
		reason := freecoding.InferenceAllowed(freecoding.InferencePolicy{})
		rec := freecoding.MakeSkip("", "", "", reason, "", now)
		if len(models) > 0 {
			rec.Note = "tasks not started: bugfix, refactor, review; no request sent; models=" + strings.Join(models, ",")
		}
		fmt.Fprintf(os.Stderr, "SKIP: %s\n", reason)
		finish(*out, []freecoding.Record{rec}, false)
		return
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "infer needs one or more provider/model-id arguments")
		os.Exit(2)
	}
	if *allowPath == "" {
		fmt.Fprintln(os.Stderr, "infer needs -allow produced by offline or discover")
		os.Exit(2)
	}
	allowRecs, err := readJSONL(*allowPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "allowlist: %v\n", err)
		os.Exit(1)
	}
	allow := freecoding.LoadAllowlist(allowRecs)
	var recs []freecoding.Record
	for _, spec := range models {
		provider, model, err := splitModel(spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		ep := *endpoint
		if ep == "" {
			ep, err = freecoding.DefaultEndpoint(provider)
			if err != nil {
				recs = append(recs, freecoding.MakeSkip(provider, model, "", err.Error(), "", now))
				fmt.Fprintf(os.Stderr, "SKIP: %s\n", err)
				continue
			}
		}
		policy := freecoding.InferencePolicy{
			Flag:     "1",
			Provider: provider,
			ModelID:  model,
			Endpoint: ep,
			APIKey:   credential(provider),
			Allow:    allow,
		}
		if reason := freecoding.InferenceAllowed(policy); reason != "" {
			recs = append(recs, freecoding.MakeSkip(provider, model, "", reason, ep, now))
			fmt.Fprintf(os.Stderr, "SKIP: %s/%s: %s\n", provider, model, reason)
			continue
		}
		for _, task := range []string{freecoding.TaskBugfix, freecoding.TaskRefactor, freecoding.TaskReview} {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			rec, err := freecoding.Infer(ctx, freecoding.InferInput{
				Policy: policy,
				Task:   task,
				Run:    *run,
				Now:    now,
			})
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "infer: %v\n", err)
				os.Exit(1)
			}
			recs = append(recs, rec)
			fmt.Printf("%s | %s | %s | pass=%v | http=%d | %dms | 429=%d\n",
				rec.Provider, rec.ModelID, rec.Task, rec.Pass, rec.HTTPStatus, rec.LatencyMs, rec.Status429Count)
		}
	}
	finish(*out, recs, false)
}

func reportCmd(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	jsonl := fs.String("jsonl", "", "runs.jsonl path")
	out := fs.String("out", "", "markdown path; default stdout")
	_ = fs.Parse(args)
	if *jsonl == "" {
		fmt.Fprintln(os.Stderr, "-jsonl is required")
		os.Exit(2)
	}
	recs, err := readJSONL(*jsonl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
	if *out == "" {
		if err := freecoding.WriteReport(os.Stdout, recs); err != nil {
			fmt.Fprintf(os.Stderr, "report: %v\n", err)
			os.Exit(1)
		}
		return
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := freecoding.WriteReport(f, recs); err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
}

func finish(dir string, recs []freecoding.Record, discover bool) {
	if err := freecoding.WriteBundle(dir, recs); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	if discover {
		eligible, trials := 0, 0
		for _, rec := range recs {
			if rec.RecordType != freecoding.RecordTrial {
				continue
			}
			trials++
			if rec.CodingProfileEligible {
				eligible++
			}
		}
		fmt.Fprintf(os.Stderr, "trial candidates=%d coding_profile_eligible=%d (not a ranking)\n", trials, eligible)
	}
	fmt.Printf("REPORTE=%s\n", filepath.Join(dir, "report.md"))
	if discover && freecoding.CatalogsFailed(recs) {
		os.Exit(1)
	}
}

func readJSONL(path string) ([]freecoding.Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return freecoding.ParseJSONL(f)
}

func splitModel(spec string) (string, string, error) {
	provider, model, ok := strings.Cut(spec, "/")
	if !ok || provider == "" || model == "" {
		return "", "", fmt.Errorf("want provider/model-id (model id may contain slashes), got %q", spec)
	}
	return provider, model, nil
}

func credential(provider string) string {
	switch provider {
	case "openrouter":
		if v := os.Getenv("OPENROUTER_FREE_API_KEY"); v != "" {
			return v
		}
	case "cline":
		if v := os.Getenv("CLINE_FREE_API_KEY"); v != "" {
			return v
		}
	}
	return os.Getenv("FREE_CODING_API_KEY")
}
