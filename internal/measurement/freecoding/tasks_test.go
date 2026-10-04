package freecoding

import (
	"strings"
	"testing"
)

func TestGradeReferenceSubmissions(t *testing.T) {
	cases := []struct {
		task          string
		file          string
		pass          bool
		compile, test *bool
	}{
		{TaskBugfix, "testdata/bugfix/pass/sum.go", true, boolPtr(true), boolPtr(true)},
		{TaskBugfix, "testdata/bugfix/fail/sum.go", false, boolPtr(true), boolPtr(false)},
		{TaskRefactor, "testdata/refactor/pass/stats.go", true, boolPtr(true), boolPtr(true)},
		{TaskRefactor, "testdata/refactor/fail/stats.go", false, boolPtr(true), boolPtr(true)},
		{TaskReview, "testdata/review/pass.txt", true, nil, nil},
		{TaskReview, "testdata/review/fail.txt", false, nil, nil},
	}
	for _, tc := range cases {
		body, err := fixtureFS.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Grade(tc.task, string(body))
		if err != nil {
			t.Fatal(err)
		}
		if got.Pass != tc.pass {
			t.Fatalf("%s pass=%v error=%s", tc.file, got.Pass, got.Error)
		}
		if !sameBool(got.CompileOK, tc.compile) || !sameBool(got.TestOK, tc.test) {
			t.Fatalf("%s compile/test %+v %+v", tc.file, got.CompileOK, got.TestOK)
		}
		if tc.task == TaskRefactor && !tc.pass && got.Error != "Average does not call Total" {
			t.Fatalf("refactor error %q", got.Error)
		}
	}
}

func TestGradeRefusesImports(t *testing.T) {
	src := "package task\nimport \"os\"\nfunc Sum(a, b int) int { return a + b }\n"
	got, err := Grade(TaskBugfix, src)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pass || !strings.Contains(got.Error, "imports") {
		t.Fatalf("%+v", got)
	}
}

func TestExtractSubmissionFence(t *testing.T) {
	body, err := fixtureFS.ReadFile("testdata/bugfix/pass/sum.go")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := "Here is the fix:\n```go\n" + string(body) + "\n```\n"
	if got := ExtractSubmission(TaskBugfix, wrapped); got != strings.TrimSpace(string(body)) {
		t.Fatalf("fence extract:\n%s", got)
	}
	review := ExtractSubmission(TaskReview, "```go\nignored\n```")
	if review != "```go\nignored\n```" {
		t.Fatalf("review rewritten: %s", review)
	}
}

func TestPlantedBugFails(t *testing.T) {
	ok, detail, err := PlantedBugFails()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("planted bug did not fail its test: %s", detail)
	}
}

func TestPromptsNameTasks(t *testing.T) {
	for _, task := range []string{TaskBugfix, TaskRefactor, TaskReview} {
		prompt, err := Prompt(task)
		if err != nil || !strings.Contains(prompt, "package task") {
			t.Fatalf("%s prompt: %v", task, err)
		}
	}
	review, err := Prompt(TaskReview)
	if err != nil || !strings.Contains(review, "ClampPositive") || !strings.Contains(review, "n > 0") {
		t.Fatal("review prompt missing planted marker")
	}
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
