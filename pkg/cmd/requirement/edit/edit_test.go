// +build unit

package edit_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jenkins-x/jx-gitops/pkg/cmd/requirement/edit"
	"github.com/jenkins-x/jx-helpers/pkg/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jenkins-x/jx-api/pkg/config"
)

func TestCmdRequirementsEdit(t *testing.T) {
	t.Parallel()

	type testData struct {
		name        string
		args        []string
		callback    func(t *testing.T, req *config.RequirementsConfig)
		fail        bool
		initialFile string
	}

	gitOpsEnabled := filepath.Join("test_data", "gitops-enabled.yml")
	tests := []testData{
		{
			name: "bbs",
			args: []string{"--git-kind=bitbucketserver"},
			callback: func(t *testing.T, req *config.RequirementsConfig) {
				assert.Equal(t, "bitbucketserver", req.Cluster.GitKind, "req.Cluster.GitKind")
				assert.True(t, req.GitOps, "req.GitOps")
			},
			initialFile: gitOpsEnabled,
		},
		{
			name: "enable-gitops",
			args: []string{"--gitops"},
			callback: func(t *testing.T, req *config.RequirementsConfig) {
				assert.True(t, req.GitOps, "req.GitOps")
			},
			initialFile: gitOpsEnabled,
		},
		{
			name: "disable-gitops",
			args: []string{"--gitops=false"},
			callback: func(t *testing.T, req *config.RequirementsConfig) {
				assert.False(t, req.GitOps, "req.GitOps")
			},
			initialFile: gitOpsEnabled,
		},
		{
			name: "bucket-logs",
			args: []string{"--bucket-logs", "gs://foo"},
			callback: func(t *testing.T, req *config.RequirementsConfig) {
				assert.Equal(t, "gs://foo", req.Storage.Logs.URL, "req.Storage.Logs.URL")
				assert.True(t, req.Storage.Logs.Enabled, "req.Storage.Logs.Enabled")
			},
			initialFile: gitOpsEnabled,
		},
		{
			name:        "bad-git-kind",
			args:        []string{"--git-kind=gitlob"},
			fail:        true,
			initialFile: gitOpsEnabled,
		},
		{
			name:        "bad-secret",
			args:        []string{"--secret=vaulx"},
			fail:        true,
			initialFile: gitOpsEnabled,
		},
	}

	tmpDir, err := ioutil.TempDir("", "jx-cmd-req-")
	require.NoError(t, err, "failed to create temp dir")
	require.DirExists(t, tmpDir, "could not create temp dir for running tests")

	for i, tt := range tests {
		if tt.name == "" {
			tt.name = fmt.Sprintf("test%d", i)
		}
		t.Logf("running test %s", tt.name)
		dir := filepath.Join(tmpDir, tt.name)

		err = os.MkdirAll(dir, files.DefaultDirWritePermissions)
		require.NoError(t, err, "failed to create dir %s", dir)

		localReqFile := filepath.Join(dir, config.RequirementsConfigFileName)
		if tt.initialFile != "" {
			err = files.CopyFile(tt.initialFile, localReqFile)
			require.NoError(t, err, "failed to copy %s to %s", tt.initialFile, localReqFile)
			require.FileExists(t, localReqFile, "file should have been copied")
		}

		cmd, _ := edit.NewCmdRequirementsEdit()
		args := append(tt.args, "--dir", dir)

		err := cmd.ParseFlags(args)
		require.NoError(t, err, "failed to parse arguments %#v for test %", args, tt.name)

		old := os.Args
		os.Args = args
		err = cmd.RunE(cmd, args)
		if err != nil {
			if tt.fail {
				t.Logf("got exected failure for test %s: %s", tt.name, err.Error())
				continue
			}
			t.Errorf("test %s reported error: %s", tt.name, err)
			continue
		}
		os.Args = old

		// now lets parse the requirements
		file := localReqFile
		require.FileExists(t, file, "should have generated the requirements file")

		req, _, err := config.LoadRequirementsConfig(dir, config.DefaultFailOnValidationError)
		require.NoError(t, err, "failed to load requirements from dir %s", dir)

		if tt.callback != nil {
			tt.callback(t, req)
		}

	}

}
