package cidetect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyKnownActions(t *testing.T) {
	cases := []struct {
		label, uses, run, wantKind string
	}{
		{"checkout", "actions/checkout@v4", "", "checkout"},
		{"setup jdk", "actions/setup-java@v4", "", "setup"},
		{"podman build", "docker/build-push-action@v6", "", "docker"},
		{"build maven", "", "mvn -DskipTests package", "build"},
		{"run tests", "", "mvn test", "test"},
		{"trivy scan", "aquasecurity/trivy-action@master", "", "security-scan"},
		{"lint", "", "eslint .", "lint"},
		{"deploy", "", "kubectl apply -f k8s.yaml", "deploy"},
		{"plain echo", "", "echo hi", "shell"},
	}
	for _, c := range cases {
		got := classify(c.label, c.uses, c.run)
		if got.Kind != c.wantKind {
			t.Errorf("%q: kind=%s want %s", c.label, got.Kind, c.wantKind)
		}
		if got.DurationSeconds <= 0 || got.CPUUtilPct <= 0 {
			t.Errorf("%q: non-positive duration/cpu: %+v", c.label, got)
		}
	}
}

func TestDetectGitHubActionsWorkflow(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `name: ci
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Setup JDK
        uses: actions/setup-java@v4
      - name: Build
        run: mvn -DskipTests package
      - name: Test
        run: mvn test
      - name: podman build
        run: podman build -t app .
`
	if err := os.WriteFile(filepath.Join(wf, "ci.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	pls, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pls) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(pls))
	}
	p := pls[0]
	if p.Platform != "github-actions" {
		t.Errorf("platform=%s", p.Platform)
	}
	if len(p.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(p.Steps))
	}
	wantKinds := []string{"checkout", "setup", "build", "test", "docker"}
	for i, s := range p.Steps {
		if s.Kind != wantKinds[i] {
			t.Errorf("step %d kind=%s want %s (name=%q)", i, s.Kind, wantKinds[i], s.Name)
		}
	}
	if p.TotalDurationSec() <= 0 {
		t.Errorf("expected positive total duration")
	}
}

func TestDetectGitLabCI(t *testing.T) {
	dir := t.TempDir()
	yaml := `stages: [build, test]
build_job:
  stage: build
  script:
    - mvn package
test_job:
  stage: test
  script:
    - mvn test
    - echo done
`
	if err := os.WriteFile(filepath.Join(dir, ".gitlab-ci.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	pls, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pls) != 1 || pls[0].Platform != "gitlab-ci" {
		t.Fatalf("expected 1 gitlab pipeline, got %+v", pls)
	}
	if len(pls[0].Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(pls[0].Steps))
	}
}

