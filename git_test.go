package git

import (
	"io/ioutil"
	"log"
	"testing"
	"time"

	"github.com/akhenakh/caddy-git/gittest"
)

// init sets the OS used to fakeOS.
func init() {
	SetOS(gittest.FakeOS)
}

func check(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Error not expected but found %v", err)
		t.FailNow()
	}
}

func TestGit(t *testing.T) {
	// prepare
	repos := []*Repo{
		nil,
		&Repo{Path: "gitdir", URL: "success.git"},
	}
	for _, r := range repos {
		repo := createRepo(r)
		err := repo.Prepare()
		check(t, err)
	}

	// pull with success
	logFile := gittest.Open("file")
	SetLogger(log.New(logFile, "", 0))
	tests := []struct {
		repo   *Repo
		output string
	}{
		{
			&Repo{Path: "gitdir", URL: "https://github.com/user/repo.git", Then: []Then{NewThen("echo", "Hello")}},
			`https://github.com/user/repo.git pulled.
Command 'echo Hello' successful.
`,
		},
		{
			&Repo{URL: "ssh://git@github.com:user/repo"},
			`ssh://git@github.com:user/repo pulled.
`,
		},
	}

	for i, test := range tests {
		gittest.CmdOutput = test.repo.URL.String()

		test.repo = createRepo(test.repo)

		err := test.repo.Prepare()
		check(t, err)

		err = test.repo.Pull()
		check(t, err)

		out, err := ioutil.ReadAll(logFile)
		check(t, err)
		if test.output != string(out) {
			t.Errorf("Pull with Success %v: Expected %v found %v", i, test.output, string(out))
		}
	}

	// pull with error
	repos = []*Repo{
		&Repo{Path: "gitdir", URL: "http://github.com:u/repo.git"},
		&Repo{Path: "gitdir", URL: "https://github.com/user/repo.git", Then: []Then{NewThen("echo", "Hello")}},
		&Repo{Path: "gitdir"},
	}

	gittest.CmdOutput = "git@github.com:u1/repo.git"
	for i, repo := range repos {
		repo = createRepo(repo)

		err := repo.Prepare()
		if err == nil {
			t.Errorf("Pull with Error %v: Error expected but not found %v", i, err)
			continue
		}

		expected := "another git repo 'git@github.com:u1/repo.git' exists at gitdir"
		if expected != err.Error() {
			t.Errorf("Pull with Error %v: Expected %v found %v", i, expected, err.Error())
		}
	}

	// timeout checks
	timeoutTests := []struct {
		repo       *Repo
		shouldPull bool
	}{
		{&Repo{Interval: time.Millisecond * 4900}, false},
		{&Repo{Interval: time.Millisecond * 1}, false},
		{&Repo{Interval: time.Second * 5}, true},
		{&Repo{Interval: time.Second * 10}, true},
	}

	for i, r := range timeoutTests {
		r.repo = createRepo(r.repo)

		err := r.repo.Prepare()
		check(t, err)
		err = r.repo.Pull()
		check(t, err)

		before := r.repo.lastPull

		gittest.Sleep(r.repo.Interval)

		err = r.repo.Pull()
		after := r.repo.lastPull
		check(t, err)

		expected := after.After(before)
		if expected != r.shouldPull {
			t.Errorf("Pull with Error %v: Expected %v found %v", i, expected, r.shouldPull)
		}
	}

}

func createRepo(r *Repo) *Repo {
	repo := &Repo{
		URL:      "git@github.com/user/test",
		Path:     ".",
		Host:     "github.com",
		Branch:   "master",
		Interval: time.Second * 60,
	}
	if r == nil {
		return repo
	}
	if r.Branch != "" {
		repo.Branch = r.Branch
	}
	if r.Host != "" {
		repo.Branch = r.Branch
	}
	if r.Interval != 0 {
		repo.Interval = r.Interval
	}
	if r.Path != "" {
		repo.Path = r.Path
	}
	if r.Then != nil {
		repo.Then = r.Then
	}
	if r.URL != "" {
		repo.URL = r.URL
	}

	return repo
}
