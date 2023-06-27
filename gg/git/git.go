package git

import (
	"fmt"
	"os/exec"
	"strings"
)

type Git struct {
	command string
	remote  string
}

func New(command string) *Git {
	return &Git{
		command: command,
		remote:  "origin",
	}
}

func (g *Git) run(args ...string) (string, error) {
	stdout, err := exec.Command(g.command, args...).Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s: %s", exitError, string(exitError.Stderr))
		}
		return "", err
	}
	return string(stdout), nil
}

func (g *Git) DefaultBranch() (string, error) {
	// can be skipped
	if _, err := g.run("remote", "set-head", g.remote, "--auto"); err != nil {
		return "", err
	}

	symbolicRef, err := g.run("symbolic-ref", fmt.Sprintf("refs/remotes/%s/HEAD", g.remote))
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(strings.TrimPrefix(symbolicRef, fmt.Sprintf("refs/remotes/%s/", g.remote))), nil
}

func (g *Git) CurrentBranch() (string, error) {
	out, err := g.run("symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *Git) SwitchBranch(branch string) error {
	_, err := g.run("switch", branch)
	return err
}

func (g *Git) CreateBranch(branch string, from string) error {
	_, err := g.run("branch", branch, from)
	return err
}

func (g *Git) DeleteBranch(branch string, force bool) error {
	var opt string
	if force {
		opt = "-D"
	} else {
		opt = "-d"
	}

	_, err := g.run("branch", opt, branch)
	return err
}

func (g *Git) PushStash() error {
	_, err := g.run("stash", "push")
	return err
}

func (g *Git) PopStash() error {
	_, err := g.run("stash", "pop")
	return err
}

func (g *Git) WorktreeAdd(path string, commit string) error {
	_, err := g.run("worktree", "add", path, commit)
	return err
}

func (g *Git) RemoveWorktree(path string) error {
	_, err := g.run("worktree", "remove", path)
	return err
}

func (g *Git) PullRebase() error {
	_, err := g.run("pull", "--rebase")
	return err
}

func (g *Git) RootDir() (string, error) {
	output, err := g.run("rev-parse", "--show-superproject-working-tree", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.Split(output, "\n")[0], nil
}

type Worktree struct {
	Dir    string
	Head   string
	Branch string
}

func (g *Git) Worktrees() ([]Worktree, error) {
	output, err := g.run("worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	for _, line := range strings.Split(output, "\000\000") {
		var wt Worktree
		for _, line := range strings.Split(line, "\000") {
			if strings.HasPrefix(line, "worktree ") {
				wt.Dir = strings.TrimPrefix(line, "worktree ")
			} else if strings.HasPrefix(line, "HEAD ") {
				wt.Head = strings.TrimPrefix(line, "HEAD ")
			} else if strings.HasPrefix(line, "branch ") {
				wt.Branch = strings.TrimPrefix(line, "branch ")
			}
		}
		if wt.Dir != "" && wt.Head != "" && wt.Branch != "" {
			worktrees = append(worktrees, wt)
		}
	}

	return worktrees, nil
}

func (g *Git) setConfig(k string, v string) error {
	_, err := g.run("config", k, v)
	return err
}

func (g *Git) getConfig(k string) (string, error) {
	return g.run("config", k)
}

func (g *Git) SetTitle(branch string, title string) error {
	return g.setConfig("branch."+branch+".title", title)
}

func (g *Git) GetTitle(branch string) (string, error) {
	return g.getConfig("branch." + branch + ".title")
}

func (g *Git) Push(branch string) error {
	_, err := g.run("push", g.remote, branch)
	return err
}
