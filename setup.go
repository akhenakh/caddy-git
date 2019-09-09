package git

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy"
	"github.com/caddyserver/caddy/caddyhttp/httpserver"
)

const (
	// DefaultInterval is the minimum interval to delay before
	// requesting another git pull
	DefaultInterval time.Duration = time.Hour * 1
)

func init() {
	caddy.RegisterPlugin("puregit", caddy.Plugin{
		ServerType: "http",
		Action:     setup,
	})
	httpserver.RegisterDevDirective("puregit", "git")
}

// setup configures a new Git service routine.
func setup(c *caddy.Controller) error {
	git, err := parse(c)
	if err != nil {
		return err
	}

	// repos configured with webhooks
	var hookRepos []*Repo

	// functions to execute at startup
	var startupFuncs []func() error

	// loop through all repos and and start monitoring
	for i := range git {
		repo := git.Repo(i)

		// If a HookUrl is set, we switch to event based pulling.
		// Install the url handler
		if repo.Hook.URL != "" {
			hookRepos = append(hookRepos, repo)
			startupFuncs = append(startupFuncs, repo.Pull)
		} else {
			startupFuncs = append(startupFuncs, func() error {
				// Start service routine in background
				Start(repo)

				// Do a pull right away to return error
				return repo.Pull()
			})
		}
	}

	// ensure the functions are executed once per server block
	// for cases like server1.com, server2.com { ... }
	c.OncePerServerBlock(func() error {
		for i := range startupFuncs {
			c.OnStartup(startupFuncs[i])
		}
		return nil
	})

	// if there are repo(s) with webhook
	// return handler
	if len(hookRepos) > 0 {
		webhook := &WebHook{Repos: hookRepos}
		httpserver.GetConfig(c).AddMiddleware(func(next httpserver.Handler) httpserver.Handler {
			webhook.Next = next
			return webhook
		})
	}

	return nil
}

func parse(c *caddy.Controller) (Git, error) {
	var git Git

	config := httpserver.GetConfig(c)
	for c.Next() {
		repo := &Repo{Branch: "master", Interval: DefaultInterval, Path: config.Root}

		args := c.RemainingArgs()

		clonePath := func(s string) string {
			if filepath.IsAbs(s) {
				return filepath.Clean(s)
			}
			return filepath.Join(config.Root, s)
		}

		switch len(args) {
		case 2:
			repo.Path = clonePath(args[1])
			fallthrough
		case 1:
			repo.URL = RepoURL(args[0])
		}

		for c.NextBlock() {
			switch c.Val() {
			case "repo":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				repo.URL = RepoURL(c.Val())
			case "path":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				repo.Path = clonePath(c.Val())
			case "branch":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				repo.Branch = c.Val()
			case "auth_token":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				repo.Token = c.Val()
			case "interval":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				t, _ := strconv.Atoi(c.Val())
				if t > 0 {
					repo.Interval = time.Duration(t) * time.Second
				}
			case "hook":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				repo.Hook.URL = c.Val()

				// optional secret for validation
				if c.NextArg() {
					repo.Hook.Secret = c.Val()
				}
			case "hook_type":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				t := c.Val()
				if _, ok := handlers[t]; !ok {
					return nil, c.Errf("invalid hook type %v", t)
				}
				repo.Hook.Type = t
			case "then":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				command := c.Val()
				args := c.RemainingArgs()
				repo.Then = append(repo.Then, NewThen(command, args...))
			case "then_long":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				command := c.Val()
				args := c.RemainingArgs()
				repo.Then = append(repo.Then, NewLongThen(command, args...))
			default:
				return nil, c.ArgErr()
			}
		}

		// if repo is not specified, return error
		if repo.URL == "" {
			return nil, c.ArgErr()
		}
		// validate repo url
		if repoURL, err := parseURL(string(repo.URL)); err != nil {
			return nil, err
		} else {
			repo.URL = RepoURL(repoURL.String())
			repo.Host = repoURL.Hostname()
		}

		// prepare repo for use
		if err := repo.Prepare(); err != nil {
			return nil, err
		}

		git = append(git, repo)
	}

	return git, nil
}

// parseURL validates if repoUrl is a valid git url.
func parseURL(repoURL string) (*url.URL, error) {
	// scheme
	urlParts := strings.Split(repoURL, "://")
	switch {
	case strings.HasPrefix(repoURL, "https://"):
	case strings.HasPrefix(repoURL, "http://"):
	case strings.HasPrefix(repoURL, "ssh://"):
	case len(urlParts) > 1:
		return nil, fmt.Errorf("invalid url scheme %s. If url contains port, scheme is required", urlParts[0])
	default:
		repoURL = "https://" + repoURL
	}

	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}
	return u, nil
}
