package helm_tester

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/stretchr/testify/assert"
	"gopkg.in/op/go-logging.v1"
	"gopkg.in/yaml.v3"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
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
)

type HelmTester struct {
	Client        *kubernetes.Clientset
	DynamicClient *dynamic.DynamicClient
	ClusterName   string
	Chart         *HelmChart

	_pods_allowed       bool
	_secrets_allowed    bool
	_daemonsets_allowed bool

	_rest_config *rest.Config

	_rendered            any
	_chart_values        chartutil.Values
	_dependencies_values DependancyValues
}

func NewHelmTester(helm_path string) *HelmTester {
	tester := &HelmTester{}
	var err error

	// Load helm values
	UpdateDependencies(helm_path)
	c, err := loader.Load(helm_path)
	if err != nil {
		log.Fatal(err)
	}

	tester.Chart = &HelmChart{c, nil}
	tester.Chart.Dependencies = tester.Chart._Dependencies()

	// Configure Kubes
	kubeconfigpath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	kubeconfig := clientcmd.GetConfigFromFileOrDie(kubeconfigpath)
	_rest_config, _ := clientcmd.BuildConfigFromFlags("", kubeconfigpath)
	tester.ClusterName = strings.Split(kubeconfig.CurrentContext, "/")[1]
	tester.Client, err = kubernetes.NewForConfig(_rest_config)
	tester.DynamicClient, _ = dynamic.NewForConfig(_rest_config)

	// Checking connectivity and correct permissions
	if err != nil {
		fmt.Printf("error getting Kubernetes clientset: %v\n", err)
		os.Exit(1)
	}

	_, err = tester.Client.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	tester._pods_allowed = err == nil

	_, err = tester.Client.CoreV1().Secrets("default").List(context.TODO(), metav1.ListOptions{})
	tester._secrets_allowed = err == nil

	_, err = tester.Client.AppsV1().DaemonSets("default").List(context.TODO(), metav1.ListOptions{})
	tester._daemonsets_allowed = err == nil

	fmt.Println("Current context: ", kubeconfig.CurrentContext)
	return tester
}

func UpdateDependencies(chartPath string) error {
	settings := cli.New()
	providers := getter.All(settings)
	chartYamlPath := filepath.Join(chartPath, "Chart.yaml")
	chartsDir := filepath.Join(chartPath, "charts")

	// Load the chart to inspect dependencies
	c, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart from %s: %w", chartPath, err)
	}

	// Check if dependencies are already downloaded
	allDepsPresent := true
	for _, dep := range c.Metadata.Dependencies {
		depPath := filepath.Join(chartsDir, fmt.Sprintf("%s-%s.tgz", dep.Name, dep.Version))
		if _, err := os.Stat(depPath); os.IsNotExist(err) {
			allDepsPresent = false
			break
		}
	}

	if allDepsPresent {
		fmt.Println("All dependencies are already downloaded.")
		return nil
	}
	fmt.Println("Updating dependencies.")

	out := io.Discard // Change to &buf if you want to capture the output

	manager := downloader.Manager{
		ChartPath:        chartPath,
		Out:              out,
		Getters:          providers,
		RepositoryConfig: settings.RepositoryConfig,
		RepositoryCache:  settings.RepositoryCache,
		Debug:            false,
	}

	if err := manager.Update(); err != nil {
		return fmt.Errorf("failed to update dependencies for chart %s: %w", chartYamlPath, err)
	}

	return nil
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

func (c *HelmChart) _DependenciesValues() []any {
	v := []any{}
	for _, d := range c.Chart.Dependencies() {
		v = append(v, d.Values)
	}
	return v
}

func (h *HelmTester) Render() (any, error) {
	_e := engine.New(h._rest_config)
	v, e := chartutil.ToRenderValues(h.Chart.Chart, h.Chart.Values, chartutil.ReleaseOptions{}, nil)
	h._chart_values = v
	if e != nil {
		return nil, e
	}

	r, e := _e.Render(h.Chart.Chart, v)
	if e != nil {
		return nil, e
	}
	m := map[string]any{}
	for k, v := range r {
		var d any
		yaml.Unmarshal([]byte(v), &d)
		m[k] = d
	}
	h._rendered = m
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
		_, e := h.Render()
		if e != nil {
			log.Printf("Query render error: %s", e)
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
		log.Fatal(err)
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
			fmt.Sprintf("%s/state", pod.Name),
			func(tt *testing.T) {
				assert.Equal(tt, corev1.PodPhase("Running"), pod.Status.Phase, "%v not running", pod.Name)
			},
		)

		t.Run(
			fmt.Sprintf("%s/image", pod.Name),
			func(tt *testing.T) {
				for _, cont := range pod.Spec.Containers {
					assert.True(tt, strings.Contains(cont.Image, image), "expecting %s got %s", image, cont.Image)
				}
			},
		)
	}
}
