package freecoding

import (
	"context"
	"embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed testdata
var fixtureFS embed.FS

// taskGoMod is written into the temporary module at grade time. It is not
// stored under testdata: a go.mod there would make Go treat the directory as
// another module and skip it during //go:embed.
const taskGoMod = "module task\n\ngo 1.22\n"

// GradeResult is the deterministic outcome of one coding task.
type GradeResult struct {
	Pass        bool
	CompileOK   *bool
	TestOK      *bool
	ReviewMatch *bool
	Error       string
}

// Prompt returns the user prompt for a comparable coding task.
func Prompt(task string) (string, error) {
	switch task {
	case TaskBugfix:
		src, err := fixtureFS.ReadFile("testdata/bugfix/fail/sum.go")
		if err != nil {
			return "", err
		}
		tests, err := fixtureFS.ReadFile("testdata/bugfix/sum_test.go")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`Fix the bug in this Go file so the tests pass. Return only the corrected sum.go source, in one go code fence. Keep package task. Do not add imports. Do not modify the tests.

%s

Tests that will be run unchanged:

%s
`, string(src), string(tests)), nil
	case TaskRefactor:
		src, err := fixtureFS.ReadFile("testdata/refactor/fail/stats.go")
		if err != nil {
			return "", err
		}
		tests, err := fixtureFS.ReadFile("testdata/refactor/stats_test.go")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`Refactor this Go file so Average calls Total and the duplicated loop is removed. Behavior must stay the same and the tests must pass. Return only the corrected stats.go source, in one go code fence. Keep package task. Do not add imports.

%s

Tests that will be run unchanged:

%s
`, string(src), string(tests)), nil
	case TaskReview:
		src, err := fixtureFS.ReadFile("testdata/review/planted.go")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`Review this Go function. A bug is planted. In your reply, name the function ClampPositive, quote the faulty condition exactly as n > 0, and either say the comparison is inverted or show the correction n < 0. Do not claim you ran tests.

%s
`, string(src)), nil
	default:
		return "", fmt.Errorf("unknown task %q", task)
	}
}

// ExtractSubmission pulls a fenced code block when the model wrapped one.
// Review text is returned unchanged.
func ExtractSubmission(task, content string) string {
	content = strings.TrimSpace(content)
	if task == TaskReview {
		return content
	}
	if body, ok := extractFence(content); ok {
		return body
	}
	return content
}

func extractFence(content string) (string, bool) {
	const fence = "```"
	start := strings.Index(content, fence)
	if start < 0 {
		return "", false
	}
	rest := content[start+len(fence):]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	rest = rest[nl+1:]
	end := strings.Index(rest, fence)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:end]), true
}

// Grade scores a submission for one task. Bugfix and refactor compile and
// test the harness tests plus, for refactor, an AST check that Average calls
// Total. Review checks required markers for the planted bug. Submissions
// that import packages or carry generate/embed/cgo directives are refused
// before execution.
func Grade(task, submission string) (GradeResult, error) {
	switch task {
	case TaskBugfix, TaskRefactor:
		return gradeCode(task, submission)
	case TaskReview:
		ok := reviewMatches(submission)
		return GradeResult{Pass: ok, ReviewMatch: boolPtr(ok)}, nil
	default:
		return GradeResult{}, fmt.Errorf("unknown task %q", task)
	}
}

func reviewMatches(text string) bool {
	if !strings.Contains(text, "ClampPositive") || !strings.Contains(text, "n > 0") {
		return false
	}
	if strings.Contains(text, "n < 0") || strings.Contains(strings.ToLower(text), "inverted") {
		return true
	}
	return false
}

func gradeCode(task, submission string) (GradeResult, error) {
	submission = strings.TrimSpace(submission)
	if len(submission) == 0 || len(submission) > 64*1024 {
		return GradeResult{CompileOK: boolPtr(false), TestOK: boolPtr(false), Error: "submission empty or larger than 64KiB"}, nil
	}
	if strings.Contains(submission, "go:embed") || strings.Contains(submission, "go:generate") || strings.Contains(submission, "import \"C\"") || strings.Contains(submission, "#cgo") {
		return GradeResult{CompileOK: boolPtr(false), TestOK: boolPtr(false), Error: "refused to execute submission with embed, generate, or cgo directives"}, nil
	}
	fset := token.NewFileSet()
	name := "sum.go"
	if task == TaskRefactor {
		name = "stats.go"
	}
	file, err := parser.ParseFile(fset, name, submission, 0)
	if err != nil {
		return GradeResult{CompileOK: boolPtr(false), TestOK: boolPtr(false), Error: truncate(err.Error(), 400)}, nil
	}
	if file.Name == nil || file.Name.Name != "task" {
		return GradeResult{CompileOK: boolPtr(false), TestOK: boolPtr(false), Error: "submission must be package task"}, nil
	}
	if len(file.Imports) > 0 {
		return GradeResult{CompileOK: boolPtr(false), TestOK: boolPtr(false), Error: "refused to execute submission that imports packages"}, nil
	}
	refactorOK := true
	if task == TaskRefactor {
		refactorOK = averageCallsTotal(file)
	}
	dir, err := os.MkdirTemp("", "freecoding-grade-")
	if err != nil {
		return GradeResult{}, err
	}
	defer os.RemoveAll(dir)
	if err := materialize(dir, task, submission); err != nil {
		return GradeResult{}, err
	}
	compileOK, testOK, detail := runGoTest(dir)
	res := GradeResult{CompileOK: boolPtr(compileOK), TestOK: boolPtr(testOK)}
	if !compileOK || !testOK {
		res.Error = truncate(detail, 400)
	}
	res.Pass = compileOK && testOK && refactorOK
	if compileOK && testOK && !refactorOK {
		res.Error = "Average does not call Total"
	}
	return res, nil
}

func averageCallsTotal(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "Average" || fn.Body == nil {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if ok && ident.Name == "Total" {
				found = true
			}
			return true
		})
		return found
	}
	return false
}

func materialize(dir, task, submission string) error {
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(taskGoMod), 0o644); err != nil {
		return err
	}
	switch task {
	case TaskBugfix:
		if err := copyEmbed(dir, "testdata/bugfix/sum_test.go", "sum_test.go"); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "sum.go"), []byte(submission), 0o644)
	case TaskRefactor:
		if err := copyEmbed(dir, "testdata/refactor/stats_test.go", "stats_test.go"); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "stats.go"), []byte(submission), 0o644)
	default:
		return fmt.Errorf("no module for task %s", task)
	}
}

func copyEmbed(dir, from, to string) error {
	b, err := fixtureFS.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, to), b, 0o644)
}

func runGoTest(dir string) (compileOK, testOK bool, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin := filepath.Join(dir, "task.test")
	compile := exec.CommandContext(ctx, "go", "test", "-c", "-o", bin, ".")
	compile.Dir = dir
	compile.Env = gradeEnv()
	out, err := compile.CombinedOutput()
	if err != nil {
		return false, false, string(out)
	}
	run := exec.CommandContext(ctx, bin)
	run.Dir = dir
	run.Env = gradeEnv()
	out, err = run.CombinedOutput()
	if err != nil {
		return true, false, string(out)
	}
	return true, true, ""
}

func gradeEnv() []string {
	env := make([]string, 0, 16)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "GOTOOLCHAIN=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GO111MODULE=on", "GOSUMDB=off", "GOTOOLCHAIN=local")
}

// PlantedBugFails is a harness integrity check: the review fixture's own
// test must fail, otherwise the planted bug is not real.
func PlantedBugFails() (bool, string, error) {
	dir, err := os.MkdirTemp("", "freecoding-planted-")
	if err != nil {
		return false, "", err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(taskGoMod), 0o644); err != nil {
		return false, "", err
	}
	for _, pair := range [][2]string{
		{"testdata/review/planted.go", "planted.go"},
		{"testdata/review/planted_test.go", "planted_test.go"},
	} {
		if err := copyEmbed(dir, pair[0], pair[1]); err != nil {
			return false, "", err
		}
	}
	compileOK, testOK, detail := runGoTest(dir)
	if !compileOK {
		return false, detail, fmt.Errorf("planted fixture did not compile")
	}
	return !testOK, detail, nil
}
