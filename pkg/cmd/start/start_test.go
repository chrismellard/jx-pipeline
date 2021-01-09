package start_test

import (
	"context"
	"testing"

	"github.com/chrismellard/jx-pipeline/pkg/cmd/start"
	"github.com/jenkins-x/go-scm/scm"
	fakescm "github.com/jenkins-x/go-scm/scm/driver/fake"
	jenkinsio "github.com/jenkins-x/jx-api/v4/pkg/apis/jenkins.io"
	jenkinsv1 "github.com/jenkins-x/jx-api/v4/pkg/apis/jenkins.io/v1"
	fakejx "github.com/jenkins-x/jx-api/v4/pkg/client/clientset/versioned/fake"
	fakelh "github.com/jenkins-x/lighthouse/pkg/client/clientset/versioned/fake"
	"github.com/jenkins-x/lighthouse/pkg/config"
	"github.com/jenkins-x/lighthouse/pkg/config/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

const fakeGitServer = "https://fake.git"

func TestPipelineStart(t *testing.T) {
	ns := "jx"
	owner := "myorg"
	repo := "myrepo"
	branch := "master"
	fullName := scm.Join(owner, repo)

	testCases := []struct {
		name       string
		shouldFail bool
		init       func(o *start.Options)
		verify     func(o *start.Options, params map[string]string)
	}{
		{
			name: "defaults",
			init: nil,
			verify: func(o *start.Options, params map[string]string) {
				assert.Equal(t, "defaultValue", params["myparam"], "myparam value")
				assert.Len(t, params, 1, "parameter count")
			},
		},
		{
			name: "presubmit-lint",
			init: func(o *start.Options) {
				o.Context = "lint"
				o.PipelineKind = "presubmit"
			},
			verify: func(o *start.Options, params map[string]string) {
				assert.Equal(t, "linter", params["prParam"], "prParam value")
			},
		},
		{
			name: "presubmit-test",
			init: func(o *start.Options) {
				o.Context = "tests"
				o.PipelineKind = "presubmit"
			},
			verify: func(o *start.Options, params map[string]string) {
				assert.Equal(t, "tester", params["prParam"], "prParam value")
			},
		},
		{
			name: "fail-on-missing-presubmit",
			init: func(o *start.Options) {
				o.Context = "does-not-exist"
				o.PipelineKind = "presubmit"
			},
			shouldFail: true,
		},
		{
			name: "fail-on-missing-postsubmit",
			init: func(o *start.Options) {
				o.Context = "does-not-exist"
			},
			shouldFail: true,
		},
		{
			name: "add-parameter",
			init: func(o *start.Options) {
				o.CustomParameters = []string{"anotherParam=myNewValue", "newParam=somethingNew"}
			},
			verify: func(o *start.Options, params map[string]string) {
				assert.Equal(t, "defaultValue", params["myparam"], "myparam value")
				assert.Equal(t, "myNewValue", params["anotherParam"], "anotherParam value")
				assert.Equal(t, "somethingNew", params["newParam"], "newParam value")
				assert.Len(t, params, 3, "parameter count")
			},
		},
	}

	scmClient, fakeScm := fakescm.NewDefault()
	fakeScm.Commits[branch] = &scm.Commit{
		Sha:     "1234",
		Message: "fix: my commit",
	}

	cfg := &config.Config{
		JobConfig: config.JobConfig{
			Postsubmits: map[string][]job.Postsubmit{
				fullName: {
					{
						Base: job.Base{
							Name:  "release",
							Agent: job.TektonPipelineAgent,
							PipelineRunSpec: &v1beta1.PipelineRunSpec{
								PipelineRef: &v1beta1.PipelineRef{
									Name:       "my-pipeline",
									APIVersion: "v1beta1",
								},
								Params: []v1beta1.Param{
									{
										Name: "myparam",
										Value: v1beta1.ArrayOrString{
											Type:      v1beta1.ParamTypeString,
											StringVal: "none",
										},
									},
									{
										Name: "anotherParam",
										Value: v1beta1.ArrayOrString{
											Type:      v1beta1.ParamTypeString,
											StringVal: "empty",
										},
									},
								},
								ServiceAccountName: "",
							},
							PipelineRunParams: []job.PipelineRunParam{
								{
									Name:          "myparam",
									ValueTemplate: "defaultValue",
								},
							},
						},
						Reporter: job.Reporter{
							Context: "release",
						},
					},
				},
			},
			Presubmits: map[string][]job.Presubmit{
				fullName: {
					{
						Base: job.Base{
							Name:  "lint",
							Agent: job.TektonPipelineAgent,
							PipelineRunSpec: &v1beta1.PipelineRunSpec{
								PipelineRef: &v1beta1.PipelineRef{
									Name:       "my-pipeline",
									APIVersion: "v1beta1",
								},
							},
							PipelineRunParams: []job.PipelineRunParam{
								{
									Name:          "prParam",
									ValueTemplate: "linter",
								},
							},
						},
						Reporter: job.Reporter{
							Context: "lint",
						},
					},
					{
						Base: job.Base{
							Name:  "tests",
							Agent: job.TektonPipelineAgent,
							PipelineRunSpec: &v1beta1.PipelineRunSpec{
								PipelineRef: &v1beta1.PipelineRef{
									Name:       "my-pipeline",
									APIVersion: "v1beta1",
								},
							},
							PipelineRunParams: []job.PipelineRunParam{
								{
									Name:          "prParam",
									ValueTemplate: "tester",
								},
							},
						},
						Reporter: job.Reporter{
							Context: "tests",
						},
					},
				},
			},
		},
	}

	configData, err := yaml.Marshal(cfg)
	require.NoError(t, err, "failed to marshal lighthouse config %v to YAML", cfg)

	for _, tc := range testCases {
		name := tc.name

		t.Logf("running test %s\n", name)

		_, o := start.NewCmdPipelineStart()

		o.ScmClients = map[string]*scm.Client{
			fakeGitServer: scmClient,
		}
		o.KubeClient = fake.NewSimpleClientset(
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      o.LighthouseConfigMap,
					Namespace: ns,
				},
				Data: map[string]string{
					"config.yaml": string(configData),
				},
			},
		)

		o.LHClient = fakelh.NewSimpleClientset()
		o.JXClient = fakejx.NewSimpleClientset(
			createGitHubSourceRepository(ns, owner, repo),
		)
		o.Namespace = ns

		o.GitUsername = "myuser"
		o.GitToken = "mytoken"
		o.Branch = branch
		o.Ctx = context.Background()

		if tc.init != nil {
			tc.init(o)
		}

		err = o.Run()
		if tc.shouldFail {
			require.Error(t, err, "should have failed for test %s", name)
			t.Logf("test %s returned expected error %s\n", name, err.Error())
			continue
		}

		require.NoError(t, err, "failed to run command for test %s", name)

		ctx := context.Background()
		lhResources, err := o.LHClient.LighthouseV1alpha1().LighthouseJobs(ns).List(ctx, metav1.ListOptions{})
		require.NoError(t, err, "should not fail to list lhjobs in namespace %s for test %s", ns, name)
		require.NotNil(t, lhResources, "no lhjob list returned in namespace %s for test %s", ns, name)
		require.Len(t, lhResources.Items, 1, "should have created a lhjob in namespace %s for test %s", ns, name)

		lhjob := lhResources.Items[0]
		require.NotEmpty(t, lhjob.Spec.PipelineRunParams, "should have pipeline run params")

		params := map[string]string{}
		for i, p := range lhjob.Spec.PipelineRunParams {
			t.Logf("test %s: param[%d] name: %s value %s\n", name, i, p.Name, p.ValueTemplate)
			params[p.Name] = p.ValueTemplate
		}

		if tc.verify != nil {
			tc.verify(o, params)
		}
	}
}

func createGitHubSourceRepository(ns, org, repo string) *jenkinsv1.SourceRepository {
	return &jenkinsv1.SourceRepository{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SourceRepository",
			APIVersion: jenkinsio.GroupName + "/" + jenkinsio.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      org + "-" + repo,
			Namespace: ns,
		},
		Spec: jenkinsv1.SourceRepositorySpec{
			Provider:     fakeGitServer,
			Org:          org,
			Repo:         repo,
			ProviderName: "fake",
			Scheduler: jenkinsv1.ResourceReference{
				Name: "cheese",
			},
		},
	}
}
