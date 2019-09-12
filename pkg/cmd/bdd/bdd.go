package bdd

import (
	"strings"
	"time"

	"github.com/jenkins-x/jx/pkg/builds"
	"github.com/jenkins-x/jx/pkg/cmd/get"
	"github.com/jenkins-x/jx/pkg/cmd/helper"
	"github.com/jenkins-x/jx/pkg/cmd/importcmd"
	"github.com/jenkins-x/jx/pkg/cmd/opts"
	"github.com/jenkins-x/jx/pkg/cmd/start"
	"github.com/jenkins-x/jx/pkg/cmd/templates"
	"github.com/jenkins-x/jx/pkg/gits"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/log"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/pkg/errors"

	"github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"
)

// StartBDDOptions contains the command line options
type StartBDDOptions struct {
	*opts.CommonOptions

	SourceGitURL string
	Branch       string
}

var (
	startBDDLong = templates.LongDesc(`
		Starts the BDD tests on the current cluster to verify the current cluster installation.

`)

	startBDDExample = templates.Examples(`
		# Start the bdd tests on the current cluster
		jx bdd
	`)
)

// NewCmdStartBDD creates the command
func NewCmdStartBDD(commonOpts *opts.CommonOptions) *cobra.Command {
	options := &StartBDDOptions{
		CommonOptions: commonOpts,
	}

	cmd := &cobra.Command{
		Use:     "bdd [flags]",
		Short:   "Starts the BDD tests on the current cluster to verify the current cluster installation",
		Long:    startBDDLong,
		Example: startBDDExample,
		Aliases: []string{"tck"},
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			helper.CheckErr(err)
		},
	}
	cmd.Flags().StringVarP(&options.SourceGitURL, "git-url", "u", "https://github.com/jenkins-x/bdd-jx.git", "The git URL of the BDD tests pipeline")
	cmd.Flags().StringVarP(&options.Branch, "branch", "", "master", "The git branch to use to run the BDD tests")
	return cmd
}

// Run implements this command
func (o *StartBDDOptions) Run() error {
	jxClient, ns, err := o.JXClientAndDevNamespace()
	if err != nil {
		return err
	}

	list, err := jxClient.JenkinsV1().SourceRepositories(ns).List(metav1.ListOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrapf(err, "failed to load SourceRepositories in namespace %s", ns)
	}
	gitInfo, err := gits.ParseGitURL(o.SourceGitURL)
	if err != nil {
		return errors.Wrapf(err, "failed to parse git URL %s", o.SourceGitURL)
	}
	sr, err := o.findSourceRepository(list.Items, o.SourceGitURL, gitInfo)
	if err != nil {
		return errors.Wrapf(err, "failed to find SourceRepositories for URL  %s", o.SourceGitURL)
	}
	owner := ""
	repo := ""
	trigger := true
	if sr == nil {
		err = o.importSourceRepository()
		if err != nil {
			return errors.Wrapf(err, "failed to find SourceRepositories for URL  %s", o.SourceGitURL)
		}
		trigger = false
	} else {
		owner = sr.Spec.Org
		repo = sr.Spec.Repo
	}
	if owner == "" {
		owner = gitInfo.Organisation
	}
	if owner == "" {
		owner = gitInfo.Organisation
	}
	if trigger {
		err = o.triggerPipeline(owner, repo)
		if err != nil {
			return errors.Wrapf(err, "failed to find SourceRepositories for URL  %s", o.SourceGitURL)
		}
	}

	// let sleep a little bit to give things a head start
	time.Sleep(time.Second * 3)

	commonOptions := *o.CommonOptions
	commonOptions.BatchMode = true
	lo := &get.GetBuildLogsOptions{
		GetOptions: get.GetOptions{
			CommonOptions: &commonOptions,
		},
		Tail:           true,
		Wait:           true,
		FailIfPodFails: true,
		BuildFilter: builds.BuildPodInfoFilter{
			Owner:      owner,
			Repository: repo,
			Branch:     o.Branch,
		},
	}
	return lo.Run()
}

func (o *StartBDDOptions) findSourceRepository(repositories []v1.SourceRepository, url string, gitInfo *gits.GitRepository) (*v1.SourceRepository, error) {
	for i := range repositories {
		repo := &repositories[i]
		u2, _ := kube.GetRepositoryGitURL(repo)
		if url == u2 || strings.TrimSuffix(url, ".git") == strings.TrimSuffix(u2, ".git") {
			return repo, nil
		}
	}
	for i := range repositories {
		repo := &repositories[i]
		if repo.Spec.Org == gitInfo.Organisation && repo.Spec.Repo == gitInfo.Name {
			return repo, nil
		}
	}
	return nil, nil
}

func (o *StartBDDOptions) importSourceRepository() error {
	log.Logger().Infof("importing project %s", util.ColorInfo(o.SourceGitURL))

	io := &importcmd.ImportOptions{
		CommonOptions:           o.CommonOptions,
		RepoURL:                 o.SourceGitURL,
		DisableDraft:            true,
		DisableJenkinsfileCheck: true,
		DisableMaven:            true,
		DisableWebhooks:         true,
	}
	err := io.Run()
	if err != nil {
		return errors.Wrapf(err, "failed to import project %s", o.SourceGitURL)
	}
	return nil
}

func (o *StartBDDOptions) triggerPipeline(owner string, repo string) error {
	pipeline := owner + "/" + repo + "/" + o.Branch
	log.Logger().Infof("triggering pipeline %s", util.ColorInfo(pipeline))

	so := &start.StartPipelineOptions{
		CommonOptions: o.CommonOptions,
		Filter:        pipeline,
		Branch:        o.Branch,
	}
	err := so.Run()
	if err != nil {
		return errors.Wrapf(err, "failed to start pipeline %s", pipeline)
	}
	return nil
}