package variables_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	scmfake "github.com/jenkins-x/go-scm/scm/driver/fake"
	jxfake "github.com/jenkins-x/jx-api/pkg/client/clientset/versioned/fake"
	"github.com/jenkins-x/jx-api/pkg/config"
	"github.com/jenkins-x/jx-gitops/pkg/cmd/variables"
	"github.com/jenkins-x/jx-helpers/pkg/cmdrunner/fakerunner"
	"github.com/jenkins-x/jx-helpers/pkg/files"
	"github.com/jenkins-x/jx-helpers/pkg/kube/jxenv"
	"github.com/jenkins-x/jx-helpers/pkg/testhelpers"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestCmdVariables(t *testing.T) {
	runner := &fakerunner.FakeRunner{}

	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err, "failed to create temp dir")

	version := "1.2.3"
	versionFile := filepath.Join(tmpDir, "VERSION")
	err = ioutil.WriteFile(versionFile, []byte(version), files.DefaultFileWritePermissions)
	require.NoError(t, err, "failed to write file %s", versionFile)

	ns := "jx"
	devEnv := jxenv.CreateDefaultDevEnvironment(ns)
	devEnv.Namespace = ns
	devEnv.Spec.Source.URL = "https://github.com/myorg/myrepo.git"

	requirements := config.NewRequirementsConfig()
	requirements.Cluster.ChartRepository = "http://bucketrepo/bucketrepo/charts/"
	data, err := yaml.Marshal(requirements)
	require.NoError(t, err, "failed to marshal requirements")
	devEnv.Spec.TeamSettings.BootRequirements = string(data)

	jxClient := jxfake.NewSimpleClientset(devEnv)
	scmFake, _ := scmfake.NewDefault()

	_, o := variables.NewCmdVariables()
	o.Dir = tmpDir
	o.CommandRunner = runner.Run
	o.JXClient = jxClient
	o.Namespace = ns
	o.BuildNumber = "5"

	o.KubeClient = fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      o.ConfigMapName,
				Namespace: ns,
			},
			Data: map[string]string{
				"docker.registry": "my-registry.com",
				"kaniko.flags":    "cheese",
			},
		},
	)
	o.Options.Owner = "myowner"
	o.Options.Repository = "myrepo"
	o.Options.Branch = "PR-23"
	o.Options.SourceURL = "https://github.com/" + o.Options.Owner + "/" + o.Options.Repository
	o.Options.ScmClient = scmFake

	err = o.Run()

	require.NoError(t, err, "failed to run the command")

	f := filepath.Join(tmpDir, o.File)
	require.FileExists(t, f, "should have generated file")
	t.Logf("generated file %s\n", f)

	testhelpers.AssertTextFilesEqual(t, filepath.Join("test_data", "expected.sh"), f, "generated file")
}
