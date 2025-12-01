package helm_tester

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/stretchr/testify/assert"
	"gopkg.in/op/go-logging.v1"
	"gopkg.in/yaml.v3"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type HelmTester struct {
	Client        *kubernetes.Clientset
	DynamicClient *dynamic.DynamicClient
	ClusterName   string
	ContextName   string
	Chart         *HelmChart

	_rendered            any
	_chart_values        chartutil.Values
	_dependencies_values DependancyValues

	restConfig *rest.Config
}

type Option func(opt *options)

type options struct {
	SkipDependencyUpdate bool
	SkipRepoUpdate       bool
	LogLevel             zerolog.Level
	Repos                map[string]string
}

func WithSkipDependencyUpdate() Option {
	return func(opt *options) {
		opt.SkipDependencyUpdate = true
	}
}

func WithSkipRepoUpdate() Option {
	return func(opt *options) {
		opt.SkipRepoUpdate = true
	}
}

func WithLogLevel(level string) Option {
	return func(opt *options) {
		opt.LogLevel, _ = zerolog.ParseLevel(level)
	}
}

func WithRepo(name, url string) Option {
	return func(opt *options) {
		if opt.Repos == nil {
			opt.Repos = make(map[string]string)
		}
		opt.Repos[name] = url
	}
}

func NewHelmTester(helm_path string, opts ...Option) (tester *HelmTester, err error) {
	tester = &HelmTester{}
	log.Trace().Msg("NewHelmTester")
	o := options{LogLevel: zerolog.InfoLevel, Repos: make(map[string]string)}
	for _, opt := range opts {
		opt(&o)
	}

	if os.Getenv("HELMHELPER_LOGLEVEL") != "" {
		o.LogLevel, _ = zerolog.ParseLevel(os.Getenv("HELMHELPER_LOGLEVEL"))
	}

	zerolog.SetGlobalLevel(o.LogLevel)

	// Configure Kubes
	kubeconfigpath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	kubeconfig := clientcmd.GetConfigFromFileOrDie(kubeconfigpath)
	if kubeconfig == nil {
		err = fmt.Errorf("filed to get kubeconfig")
		log.Error().Err(err).Msg("Failed to get kubeconfig")
		return
	}
	log.Trace().Msg("Got kubeconfig")

	tester.ContextName = kubeconfig.CurrentContext

	if strings.Contains(kubeconfig.CurrentContext, "aws") {
		tester.ClusterName = strings.Split(kubeconfig.CurrentContext, "/")[1]
	} else if strings.Contains(kubeconfig.CurrentContext, "gke") {
		tester.ClusterName = strings.Split(kubeconfig.CurrentContext, "_")[3]
	} else {
		tester.ClusterName = kubeconfig.CurrentContext
	}

	_rest_config, err := clientcmd.BuildConfigFromFlags("", kubeconfigpath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build config")
		return
	}
	tester.restConfig = _rest_config
	tester.Client, err = kubernetes.NewForConfig(_rest_config)
	tester.DynamicClient, _ = dynamic.NewForConfig(_rest_config)
	log.Trace().Msg("Kube clients")

	// Checking connectivity and correct permissions
	if err != nil {
		log.Error().Err(err).Msg("Failed to create client")
		os.Exit(1)
	}

	log.Info().Msg("Current context: " + kubeconfig.CurrentContext)

	// Load Chart
	log.Trace().Msg("Loading chart")
	c, err := loader.Load(helm_path)
	if err != nil {
		log.Error().Err(err).Str("path", helm_path).Msg("Failed to load Chart")
	}

	settings := cli.New()
	settings.RepositoryCache = filepath.Join(os.TempDir(), "cache")
	settings.RepositoryConfig = filepath.Join(os.TempDir(), "config")
	os.MkdirAll(settings.RepositoryCache, 0755)
	os.MkdirAll(settings.RepositoryConfig, 0755)
	repoFile := filepath.Join(settings.RepositoryConfig, "repositories.yaml")

	if len(o.Repos) > 0 {
		repoFileContent := repo.File{
			APIVersion: "v1",
		}
		for name, url := range o.Repos {
			repoFileContent.Repositories = append(repoFileContent.Repositories, &repo.Entry{
				Name: name,
				URL:  url,
			})
		}
		repoBytes, _ := yaml.Marshal(repoFileContent)
		os.WriteFile(repoFile, repoBytes, 0644)
		log.Trace().Str("repositories", repoFile)
	}

	if !o.SkipDependencyUpdate {

		err := UpdateDependencies(c, helm_path, settings, o.SkipRepoUpdate)
		if err != nil {
			log.Error().Err(err).Msg("Failed to update dependencies")
		}
	} else {
		log.Info().Msg("Skipping dependencies update.")
	}
	tester.Chart = &HelmChart{c, nil}
	tester.Chart.Dependencies = tester.Chart._Dependencies()
	return
}

func UpdateDependencies(c *chart.Chart, path string, settings *cli.EnvSettings, skipRepoUpdate bool) (err error) {
	providers := getter.All(settings)
	// chartYamlPath := filepath.Join(path, "Chart.yaml")
	//_ := filepath.Join(c.ChartPath(), "Chart.lock")
	chartsDir := filepath.Join(path, "charts")

	log.Trace().Msg("Checking dependancies")
	// Check if dependencies are already downloaded
	allDepsPresent := true
	for _, dep := range c.Metadata.Dependencies {
		log.Trace().Msg("Checking " + dep.Name)
		depPath := filepath.Join(chartsDir, fmt.Sprintf("%s-%s.tgz", dep.Name, strings.TrimLeft(dep.Version, "v")))
		if _, err := os.Stat(depPath); os.IsNotExist(err) {
			allDepsPresent = false
			log.Info().Msg("Missing dependency: " + dep.Name + " " + dep.Version)
			AddRepo(dep, settings)
			break
		}
	}

	if allDepsPresent {
		log.Info().Msg("All dependencies are already downloaded.")
		return
	}
	log.Info().Msg("Updating dependencies.")

	out := io.Discard // Change to &buf if you want to capture the output

	manager := downloader.Manager{
		ChartPath:        path,
		Out:              out,
		Getters:          providers,
		SkipUpdate:       skipRepoUpdate,
		RepositoryConfig: filepath.Join(settings.RepositoryConfig, "repositories.yaml"),
	}

	log.Trace().Msg("Updating...")

	// // manager.Update() will update all repos. We use Build() instead to only download missing dependencies.
	// if err = manager.UpdateRepositories(); err != nil {
	// 	log.Error().Err(err).Msg("Failed to update repositories")
	// }
	if err = manager.Build(); err != nil {
		log.Error().Err(err).Msg("Failed to update dependencies")
	}

	return
}

func AddRepo(dep *chart.Dependency, settings *cli.EnvSettings) {
	repoFile := filepath.Join(settings.RepositoryConfig, "repositories.yaml")
	repoFileContent := &repo.File{}

	if _, err := os.Stat(repoFile); err == nil {
		repoBytes, err := os.ReadFile(repoFile)
		if err == nil {
			yaml.Unmarshal(repoBytes, repoFileContent)
		}
	}

	if repoFileContent.APIVersion == "" {
		repoFileContent.APIVersion = "v1"
	}

	for _, r := range repoFileContent.Repositories {
		if r.URL == dep.Repository {
			log.Trace().Str("repo", dep.Repository).Msg("Repository already exists")
			return
		}
	}

	log.Info().Str("name", dep.Name).Str("url", dep.Repository).Msg("Adding repository")
	repoFileContent.Repositories = append(repoFileContent.Repositories, &repo.Entry{
		Name: dep.Name,
		URL:  dep.Repository,
	})
	repoBytes, _ := yaml.Marshal(repoFileContent)
	os.WriteFile(repoFile, repoBytes, 0644)
}

func GetDefaultValues(chartPath string) (map[string]interface{}, error) {
	// Load chart from filesystem
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, err
	}

	// Return default values
	return chart.Values, nil
}

type HelmChart struct {
	*chart.Chart
	Dependencies []*HelmChart
}

func (c *HelmChart) _Dependencies() []*HelmChart {
	h := []*HelmChart{}
	for _, d := range c.Chart.Dependencies() {
		h = append(h, &HelmChart{d, nil})
	}
	return h
}

func (h *HelmTester) Render(values map[string]interface{}) (any, error) {
	_e := engine.New(h.restConfig)
	v, e := chartutil.ToRenderValues(h.Chart.Chart, values, chartutil.ReleaseOptions{}, nil)
	h._chart_values = v
	if e != nil {
		log.Error().Err(e).Msg("Render values error")
		return nil, e
	}
	log.Trace().Msg("Rendered values")

	log.Trace().Msg("Render manifests")
	r, e := _e.Render(h.Chart.Chart, v)
	if e != nil {
		log.Error().Err(e).Msg("Render error")
		return nil, e
	}
	log.Trace().Int("manifests", len(r)).Msg("Rendered")
	m := []any{}
	for _, v := range r {
		reader := strings.NewReader(v)
		dec := yaml.NewDecoder(reader)
		var node yaml.Node
		for {
			err := dec.Decode(&node)
			if err != nil {
				// Typically, you'll hit io.EOF when there are no more documents
				break
			}
			var data any
			node.Decode(&data)
			m = append(m, data)
		}
	}

	h._rendered = m
	log.Trace().Int("manifests", len(m)).Msg("Rendered in helper")
	return m, nil
}

func (h *HelmTester) AssertQueryTrue(t *testing.T, query string, msg string, args ...any) {
	r, e := h.Query(query)
	if assert.NoError(t, e, "Query error") {
		assert.Equalf(t, "true", strings.TrimRight(r, "\n"), msg, args...)
	}
}

type DependancyValues []chartutil.Values

func (vs DependancyValues) AsMaps() []any {
	v := []any{}
	for _, vv := range vs {
		v = append(v, vv.AsMap())
	}
	return v
}

func (h *HelmTester) Query(query string) (string, error) {
	if h._rendered == nil {
		_, e := h.Render(nil)
		if e != nil {
			log.Error().Err(e).Msg("Query render error")
			return "", e
		}
	}
	if h._dependencies_values == nil {
		h._dependencies_values = DependancyValues{}
		for _, hc := range h.Chart.Dependencies {
			v, _ := chartutil.ToRenderValues(hc.Chart, hc.Chart.Values, chartutil.ReleaseOptions{}, nil)
			h._dependencies_values = append(h._dependencies_values, v)
		}
	}
	data := map[string]any{
		"Chart":        h._chart_values.AsMap(),
		"Dependencies": h._dependencies_values.AsMaps(),
		"Manifests":    h._rendered,
	}

	// data := map[string]any{"name": "ricardo"}

	logging.SetLevel(logging.CRITICAL, "yq-lib")

	yamlData, err := yaml.Marshal(data)
	if err != nil {
		log.Error().Err(err).Send()
	}

	_decoder := yqlib.NewYamlDecoder(yqlib.ConfiguredYamlPreferences)
	_encoder := yqlib.NewYamlEncoder(yqlib.ConfiguredYamlPreferences)

	result, err := yqlib.NewStringEvaluator().EvaluateAll(query, string(yamlData), _encoder, _decoder)

	if err != nil {
		return "", err
	}

	return strings.TrimRight(result, "\n"), nil
}

func (h *HelmTester) CheckPermissions(verb, resource, group, version, ns string) bool {
	sar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Verb:      verb,     // Replace with the desired verb (e.g., create, update, delete)
				Group:     group,    // Replace with the API group of the resource
				Version:   version,  // Replace with the API version of the resource
				Resource:  resource, // Replace with the resource name
				Namespace: ns,       // Replace with the namespace of the resource (or omit for cluster-scoped resources)
			},
		},
	}

	response, err := h.Client.AuthorizationV1().SelfSubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return false
	}

	return response.Status.Allowed
}

func (h *HelmTester) AssertDaemonSet(t *testing.T, ns, name string) {
	ds, err := h.Client.AppsV1().DaemonSets(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("%v", err)
		return
	}

	desired := ds.Status.DesiredNumberScheduled
	available := ds.Status.NumberReady

	if available != desired {
		assert.EqualValues(t, available, desired, "not all pods running")
	}
}

func (h *HelmTester) AssertPodsUsingImage(t *testing.T, ns, labels, image string, containers ...string) {
	listOptions := metav1.ListOptions{
		LabelSelector: labels,
	}

	pods, err := h.Client.CoreV1().Pods(ns).List(context.Background(), listOptions)
	if err != nil {
		t.Errorf("unable to get pods: %v", err)
		return
	}

	assert.Greater(t, len(pods.Items), 0, "No matching pods found")

	for _, pod := range pods.Items {
		t.Run(
			pod.Name,
			func(tt *testing.T) {
				for _, status := range pod.Status.ContainerStatuses {
					assert.True(tt, status.Ready, "%s is not ready", status.Name)
					if len(containers) > 0 {
						for _, c := range containers {
							if status.Name == c {
								tt.Run(status.Name, func(ttt *testing.T) {
									assert.Equal(ttt, image, status.Image, "Image not expected")
								})
								break
							}
						}
					} else {
						tt.Run(status.Name, func(ttt *testing.T) {
							assert.Equal(ttt, image, status.Image, "Image not expected")
						})
					}
				}
			},
		)
	}
}

func (h *HelmTester) YQ(query string, target any, data ...any) error {
	var data_string string
	if len(data) == 0 {
		if h._rendered == nil {
			_, e := h.Render(nil)
			if e != nil {
				log.Error().Err(e).Msg("Query render error")
				return e
			}
		}
		if h._dependencies_values == nil {
			h._dependencies_values = DependancyValues{}
			for _, hc := range h.Chart.Dependencies {
				v, _ := chartutil.ToRenderValues(hc.Chart, hc.Chart.Values, chartutil.ReleaseOptions{}, nil)
				h._dependencies_values = append(h._dependencies_values, v)
			}
		}
		yamlData := map[string]any{
			"Chart":        h._chart_values.AsMap(),
			"Dependencies": h._dependencies_values.AsMaps(),
			"Manifests":    h._rendered,
		}
		yamlBytes, err := yaml.Marshal(yamlData)
		if err != nil {
			log.Error().Err(err).Caller().Msg("Failed to marshal data")
		}
		data_string = string(yamlBytes)
	} else if s, ok := data[0].(string); ok {
		data_string = s
	} else {
		yamlBytes, err := yaml.Marshal(data[0])
		if err != nil {
			log.Error().Err(err).Send()
		}
		data_string = string(yamlBytes)

	}

	logging.SetLevel(logging.CRITICAL, "yq-lib")

	_decoder := yqlib.NewYamlDecoder(yqlib.ConfiguredYamlPreferences)
	_encoder := yqlib.NewYamlEncoder(yqlib.ConfiguredYamlPreferences)

	result, err := yqlib.NewStringEvaluator().EvaluateAll(query, data_string, _encoder, _decoder)

	if err != nil {
		return err
	}

	err = yaml.Unmarshal([]byte(result), target)
	if err != nil {
		return err
	}
	return nil
}

func (h *HelmTester) YQString(query string, data ...any) string {
	var v string
	if err := h.YQ(query, &v, data); err != nil {
		log.Error().Err(err).Str("query", query).Msg("Failed to query string")
	}
	return v
}
