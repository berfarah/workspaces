package internal

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/berfarah/knoch/internal/command"
	"github.com/berfarah/knoch/internal/config"
	"github.com/berfarah/knoch/internal/git"
	"github.com/berfarah/knoch/internal/utils"
)

func init() {
	Runner.RegisterDefault(&command.Command{
		Run: runBundle,

		Usage: "bundle",
		Name:  "bundle",
		Long:  "Download all repositories",
	})
}

func runBundle(c *command.Command, r *command.Runtime) {
	count := len(r.Config.Projects)
	done := make(chan bool, 1)
	projects := make(chan config.Project, count)
	results := make(chan bundleStatus, count)
	bundleProgress := newBundleProgress(count)

	go bundleProgress.Track(results, done)
	for w := 0; w < r.Config.Workers(); w++ {
		go bundleWorker(w, projects, results)
	}
	for _, project := range r.Config.Projects {
		projects <- project
	}
	close(projects)

	<-done
}

func bundleWorker(id int, projects <-chan config.Project, results chan<- bundleStatus) {
	for project := range projects {
		if utils.IsDir(project.Path()) {
			err := git.Sync(project.Path())
			results <- bundleStatus{Repo: project.Repo, Sync: true, Error: err}
		} else {
			err := git.New().WithArgs("clone", "--quiet", project.Repo, project.Path()).Run()
			results <- bundleStatus{Repo: project.Repo, Download: true, Error: err}
		}
	}
}

type bundleStatus struct {
	Sync     bool
	Download bool
	Repo     string
	Error    error
}

func (s bundleStatus) Success() bool {
	return s.Error == nil
}

type bundleProgress struct {
	current  int
	sync     int
	download int
	failed   int
	total    int
}

func newBundleProgress(total int) *bundleProgress {
	return &bundleProgress{
		current:  0,
		sync:     0,
		download: 0,
		failed:   0,
		total:    total,
	}
}

func (p *bundleProgress) Track(results <-chan bundleStatus, done chan<- bool) {
	p.report()
	errors := []bundleStatus{}

	for s := 0; s < p.total; s++ {
		bundleStatus := <-results
		p.current++

		if bundleStatus.Error != nil {
			p.failed++
			errors = append(errors, bundleStatus)
		} else {
			if bundleStatus.Download {
				p.download++
			}

			if bundleStatus.Sync {
				p.sync++
			}
		}

		p.report()
	}

	utils.Println("")

	if len(errors) > 0 {
		utils.Errorln("\nErrors:")
	}

	for _, bundleStatus := range errors {
		if serr, ok := bundleStatus.Error.(*exec.ExitError); ok {
			stderr := strings.Split(string(serr.Stderr), "\n")
			if len(stderr) < 1 {
				utils.Errorln(bundleStatus.Repo, serr)
				continue
			}
			for _, line := range stderr {
				if strings.HasPrefix(line, "fatal:") {
					utils.Errorln(bundleStatus.Repo, "-", line)
				}
			}
		} else {
			utils.Errorln(bundleStatus.Repo, bundleStatus.Error)
		}
	}

	done <- true
}

func (p bundleProgress) report() {
	text := fmt.Sprintf(
		"\r[%v/%v] %v clone %v sync %v error",
		p.current,
		p.total,
		p.download,
		p.sync,
		p.failed,
	)
	utils.Print(text)
}
