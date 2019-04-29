package git

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/http"
)

const (
	// Number of retries if git pull fails
	numRetries = 3

	// variable for latest tag
	latestTag = "{latest}"
)

// Git represent multiple repositories.
type Git []*Repo

// Repo retrieves repository at i or nil if not found.
func (g Git) Repo(i int) *Repo {
	if i < len(g) {
		return g[i]
	}
	return nil
}

// RepoURL is the repository url.
type RepoURL string

// String satisfies stringer and attempts to strip off authentication
// info from url if it exists.
func (r RepoURL) String() string {
	u, err := url.Parse(string(r))
	if err != nil {
		return string(r)
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

// Val returns git friendly Val that can be
// passed to git clone.
func (r RepoURL) Val() string {
	if strings.HasPrefix(string(r), "ssh://") {
		return strings.TrimPrefix(string(r), "ssh://")
	}
	return string(r)
}

// Repo is the structure that holds required information
// of a git repository.
type Repo struct {
	URL        RepoURL       // Repository URL
	Path       string        // Directory to pull to
	Host       string        // Git domain host e.g. github.com
	Branch     string        // Git branch
	Token      string        // Authentication token
	Interval   time.Duration // Interval between pulls
	Then       []Then        // Commands to execute after successful git pull
	pulled     bool          // true if there was a successful pull
	lastPull   time.Time     // time of the last successful pull
	lastCommit string        // hash for the most recent commit
	latestTag  string        // latest tag name
	Hook       HookConfig    // Webhook configuration
	sync.Mutex
}

// Pull attempts a git pull.
// It retries at most numRetries times if error occurs
func (r *Repo) Pull() error {
	r.Lock()
	defer r.Unlock()

	// prevent a pull if the last one was less than 5 seconds ago
	if gos.TimeSince(r.lastPull) < 5*time.Second {
		return nil
	}

	// keep last commit hash for comparison later
	lastCommit := r.lastCommit

	var err error
	// Attempt to pull at most numRetries times
	for i := 0; i < numRetries; i++ {
		if err = r.pull(); err == nil {
			break
		}
		Logger().Println(err)
	}

	if err != nil {
		return err
	}

	// check if there are new changes,
	// then execute post pull command
	if r.lastCommit == lastCommit {
		Logger().Println("No new changes.")
		return nil
	}
	return r.execThen()
}

// pull performs git pull, or git clone if repository does not exist.
func (r *Repo) pull() error {
	// if not pulled, perform clone
	if !r.pulled {
		return r.clone()
	}

	gr, err := git.PlainOpen(r.Path)
	if err != nil {
		return err
	}

	w, err := gr.Worktree()
	if err != nil {
		return err
	}

	var auth *http.BasicAuth
	if r.Token != "" {
		auth = &http.BasicAuth{
			Username: "minigit", // anything except an empty string
			Password: r.Token,
		}
	}
	err = w.Pull(&git.PullOptions{
		Auth:          auth,
		RemoteName:    "origin",
		ReferenceName: plumbing.ReferenceName("refs/heads/" + r.Branch),
	})
	if err != git.NoErrAlreadyUpToDate {
		return err
	}
	ref, err := gr.Head()
	if err != nil {
		return err
	}
	commit, err := gr.CommitObject(ref.Hash())
	if err != nil {
		return err
	}

	r.pulled = true
	r.lastPull = time.Now()
	Logger().Printf("%v pulled.\n", r.URL)
	r.lastCommit = commit.String()

	return nil
}

// clone performs git clone.
func (r *Repo) clone() error {
	var auth *http.BasicAuth

	if r.Token != "" {
		auth = &http.BasicAuth{
			Username: "minigit", // anything except an empty string
			Password: r.Token,
		}
	}

	gr, err := git.PlainClone(r.Path, false, &git.CloneOptions{
		URL:               r.URL.Val(),
		Auth:              auth,
		ReferenceName:     plumbing.ReferenceName("refs/heads/" + r.Branch),
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	})
	if err != nil {
		return err
	}

	ref, err := gr.Head()
	if err != nil {
		return err
	}

	commit, err := gr.CommitObject(ref.Hash())
	if err != nil {
		return err
	}

	r.pulled = true
	r.lastPull = time.Now()
	Logger().Printf("%v pulled.\n", r.URL)
	r.lastCommit = commit.String()

	return nil
}

// checkoutCommit checks out the specified commitHash.
func (r *Repo) checkoutCommit(commitHash string) error {
	gr, err := git.PlainOpen(r.Path)
	if err != nil {
		return err
	}

	w, err := gr.Worktree()
	if err != nil {
		return err
	}

	return w.Checkout(&git.CheckoutOptions{
		Hash: plumbing.NewHash(commitHash),
	})
}

// Prepare prepares for a git pull
// and validates the configured directory
func (r *Repo) Prepare() error {
	// check if directory exists or is empty
	// if not, create directory
	fs, err := gos.ReadDir(r.Path)
	if err != nil || len(fs) == 0 {
		return gos.MkdirAll(r.Path, os.FileMode(0755))
	}

	// validate git repo
	isGit := false
	for _, f := range fs {
		if f.IsDir() && f.Name() == ".git" {
			isGit = true
			break
		}
	}

	if isGit {
		// check if same repository
		var repoURL string
		if repoURL, err = r.originURL(); err == nil {
			if strings.TrimSuffix(repoURL, ".git") == strings.TrimSuffix(r.URL.Val(), ".git") {
				r.pulled = true
				return nil
			}
		}
		if err != nil {
			return fmt.Errorf("cannot retrieve repo url for %v Error: %v", r.Path, err)
		}
		return fmt.Errorf("another git repo '%v' exists at %v", repoURL, r.Path)
	}
	return fmt.Errorf("cannot git clone into %v, directory not empty", r.Path)
}

// originURL retrieves remote origin url for the git repository at path
func (r *Repo) originURL() (string, error) {
	gr, err := git.PlainOpen(r.Path)
	if err != nil {
		return "", err
	}

	url, err := gr.Remote("origin")
	if err != nil {
		return "", err
	}
	return url.String(), err
}

// execThen executes r.Then.
// It is trigged after successful git pull
func (r *Repo) execThen() error {
	var errs error
	for _, command := range r.Then {
		err := command.Exec(r.Path)
		if err == nil {
			Logger().Printf("Command '%v' successful.\n", command.Command())
		}
		errs = mergeErrors(errs, err)
	}
	return errs
}

func mergeErrors(errs ...error) error {
	if len(errs) == 0 {
		return nil
	}
	var err error
	for _, e := range errs {
		if err == nil {
			err = e
			continue
		}
		if e != nil {
			err = fmt.Errorf("%v\n%v", err.Error(), e.Error())
		}
	}
	return err
}
