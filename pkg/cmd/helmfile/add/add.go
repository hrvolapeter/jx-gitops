package add

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jenkins-x/jx-gitops/pkg/jxtmpl/reqvalues"
	"github.com/jenkins-x/jx-gitops/pkg/versionstreamer"
	"github.com/jenkins-x/jx-helpers/pkg/gitclient"
	"github.com/jenkins-x/jx-helpers/pkg/gitclient/cli"
	"github.com/jenkins-x/jx-helpers/pkg/options"
	"github.com/jenkins-x/jx-helpers/pkg/yaml2s"
	"github.com/roboll/helmfile/pkg/state"

	"github.com/jenkins-x/jx-gitops/pkg/rootcmd"
	"github.com/jenkins-x/jx-helpers/pkg/cobras/helper"
	"github.com/jenkins-x/jx-helpers/pkg/cobras/templates"
	"github.com/jenkins-x/jx-helpers/pkg/versionstream"
	"github.com/jenkins-x/jx-logging/pkg/log"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	cmdLong = templates.LongDesc(`
		Adds a chart to the local 'helmfile.yaml' file
`)

	cmdExample = templates.Examples(`
		# adds a chart using the currently known repositories in the verison stream or helmfile.yaml
		%s helmfile add --chart somerepo/mychart

		# adds a chart using a new repository URL with a custom version and namespace
		%s helmfile add --chart somerepo/mychart --repository https://acme.com/myrepo --namespace foo --version 1.2.3
	`)
)

// Options the options for the command
type Options struct {
	versionstreamer.Options
	Namespace        string
	GitCommitMessage string
	Helmfile         string
	Chart            string
	Repository       string
	Version          string
	ReleaseName      string
	BatchMode        bool
	DoGitCommit      bool
	Gitter           gitclient.Interface
	prefixes         *versionstream.RepositoryPrefixes
	Results          Results
}

type Results struct {
	HelmState                  state.HelmState
	RequirementsValuesFileName string
}

// NewCmdHelmfileAdd creates a command object for the command
func NewCmdHelmfileAdd() (*cobra.Command, *Options) {
	o := &Options{}

	cmd := &cobra.Command{
		Use:     "add",
		Short:   "Adds a chart to the local 'helmfile.yaml' file",
		Long:    cmdLong,
		Example: fmt.Sprintf(cmdExample, rootcmd.BinaryName, rootcmd.BinaryName),
		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			helper.CheckErr(err)
		},
	}
	o.Options.AddFlags(cmd)

	cmd.Flags().StringVarP(&o.Helmfile, "helmfile", "", "", "the helmfile to resolve. If not specified defaults to 'helmfile.yaml' in the dir")
	cmd.Flags().StringVarP(&o.GitCommitMessage, "commit-message", "", "chore: generated kubernetes resources from helm chart", "the git commit message used")

	// chart flags
	cmd.Flags().StringVarP(&o.Chart, "chart", "c", "", "the name of the helm chart to add")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "the namespace to install the chart")
	cmd.Flags().StringVarP(&o.ReleaseName, "name", "", "", "the name of the helm release")
	cmd.Flags().StringVarP(&o.Repository, "repository", "r", "", "the helm chart repository URL of the chart")
	cmd.Flags().StringVarP(&o.Version, "version", "v", "", "the version of the helm chart. If not specified the versionStream will be checked otherwise the latest version is used")

	// git commit stuff....
	cmd.Flags().BoolVarP(&o.DoGitCommit, "git-commit", "", false, "if set then the template command will git commit the modified helmfile.yaml files")

	return cmd, o
}

// Validate validates the options and populates any missing values
func (o *Options) Validate() error {
	err := o.Options.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate")
	}

	if o.Chart == "" {
		return options.MissingOption("chart")
	}
	if o.Helmfile == "" {
		o.Helmfile = filepath.Join(o.Dir, "helmfile.yaml")
	}

	o.prefixes, err = o.Options.Resolver.GetRepositoryPrefixes()
	if err != nil {
		return errors.Wrapf(err, "failed to load repository prefixes at %s", o.VersionStreamDir)
	}

	err = yaml2s.LoadFile(o.Helmfile, &o.Results.HelmState)
	if err != nil {
		return errors.Wrapf(err, "failed to load helmfile %s", o.Helmfile)
	}

	if o.GitCommitMessage == "" {
		o.GitCommitMessage = "chore: resolved charts and values from the version stream"
	}

	o.prefixes, err = o.Options.Resolver.GetRepositoryPrefixes()
	if err != nil {
		return errors.Wrapf(err, "failed to load repository prefixes at %s", o.VersionStreamDir)
	}

	jxReqValuesFileName := filepath.Join(o.Dir, reqvalues.RequirementsValuesFileName)
	o.Results.RequirementsValuesFileName = reqvalues.RequirementsValuesFileName
	err = reqvalues.SaveRequirementsValuesFile(o.Options.Requirements, jxReqValuesFileName)
	if err != nil {
		return errors.Wrapf(err, "failed to save tempo file for jx requirements values file %s", jxReqValuesFileName)
	}
	return nil
}

// Run implements the command
func (o *Options) Run() error {
	err := o.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to ")
	}

	resolver := o.Options.Resolver
	if resolver == nil {
		return errors.Errorf("failed to create the VersionResolver")
	}

	helmState := o.Results.HelmState

	modified := false
	found := false

	parts := strings.Split(o.Chart, "/")
	prefix := ""
	if len(parts) > 1 {
		prefix = parts[0]
	}
	repository := o.Repository

	// lets resolve the chart prefix from a local repository from the file or from a
	// prefix in the versions stream
	if repository == "" && prefix != "" {
		for _, r := range helmState.Repositories {
			if r.Name == prefix {
				repository = r.URL
			}
		}
	}
	if repository == "" && prefix != "" {
		repository, err = versionstreamer.MatchRepositoryPrefix(o.prefixes, prefix)
		if err != nil {
			return errors.Wrapf(err, "failed to match prefix %s with repositories from versionstream %s", prefix, o.VersionStreamURL)
		}
	}
	if repository == "" && prefix != "" {
		return errors.Wrapf(err, "failed to find repository URL, not defined in helmfile.yaml or versionstream %s", o.VersionStreamURL)
	}
	if repository != "" && prefix != "" {
		// lets ensure we've got a repository for this URL in the apps file
		found := false
		for _, r := range helmState.Repositories {
			if r.Name == prefix {
				if r.URL != repository {
					return errors.Errorf("release %s has prefix %s for repository URL %s which is also mapped to prefix %s", o.Chart, prefix, r.URL, r.Name)
				}
				found = true
				break
			}
		}
		if !found {
			helmState.Repositories = append(helmState.Repositories, state.RepositorySpec{
				Name: prefix,
				URL:  repository,
			})
		}
	}

	for i := range helmState.Releases {
		release := helmState.Releases[i]
		if release.Chart == o.Chart {
			found = true
			if release.Namespace != "" && release.Namespace != o.Namespace {
				release.Namespace = o.Namespace
				modified = true
			}
		}
	}
	if !found {
		helmState.Releases = append(helmState.Releases, state.ReleaseSpec{
			Chart:     o.Chart,
			Version:   o.Version,
			Name:      o.ReleaseName,
			Namespace: o.Namespace,
		})
		modified = true
	}
	if !modified {
		log.Logger().Infof("no changes were made to file %s", o.Helmfile)
		return nil
	}

	err = yaml2s.SaveFile(helmState, o.Helmfile)
	if err != nil {
		return errors.Wrapf(err, "failed to save file %s", o.Helmfile)
	}

	if !o.DoGitCommit {
		return nil
	}
	log.Logger().Infof("committing changes: %s", o.GitCommitMessage)
	err = o.GitCommit(o.Dir, o.GitCommitMessage)
	if err != nil {
		return errors.Wrapf(err, "failed to commit changes")
	}
	return nil
}

// Git returns the gitter - lazily creating one if required
func (o *Options) Git() gitclient.Interface {
	if o.Gitter == nil {
		o.Gitter = cli.NewCLIClient("", o.CommandRunner)
	}
	return o.Gitter
}

func (o *Options) GitCommit(outDir string, commitMessage string) error {
	gitter := o.Git()
	_, err := gitter.Command(outDir, "add", "*")
	if err != nil {
		return errors.Wrapf(err, "failed to add generated resources to git in dir %s", outDir)
	}
	err = gitclient.CommitIfChanges(gitter, outDir, commitMessage)
	if err != nil {
		return errors.Wrapf(err, "failed to commit changes to git in dir %s", outDir)
	}
	return nil
}
