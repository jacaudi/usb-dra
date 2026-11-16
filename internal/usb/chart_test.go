package usb

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

// chartDir is the chart under test, relative to this package.
const chartDir = "../../charts"

// buildChartDeps vendors the chart's common dependency into charts/charts/ once per test binary. "build", not "update", so Chart.lock is honoured.
var buildChartDeps = sync.OnceValue(func() error {
	out, err := exec.Command("helm", "dependency", "build", chartDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm dependency build: %w\n%s", err, out)
	}
	return nil
})

// renderChart runs `helm template` on the chart under test and returns the decoded documents, or the helm error with stderr attached. A missing helm binary or a failed dependency build fails the test, so a rejection case can never pass for the wrong reason.
func renderChart(t *testing.T, release, namespace string, extra ...string) ([]unstructured.Unstructured, error) {
	t.Helper()
	if err := buildChartDeps(); err != nil {
		t.Fatal(err)
	}
	return renderChartAt(t, chartDir, release, namespace, extra...)
}

// renderChartAt is renderChart for an already-built chart directory.
func renderChartAt(t *testing.T, dir, release, namespace string, extra ...string) ([]unstructured.Unstructured, error) {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Fatalf("helm is required to test the chart: %v", err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(t.Context(), helm, append([]string{"template", release, dir, "-n", namespace}, extra...)...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template: %w\n%s", err, stderr.String())
	}
	r := yaml.NewYAMLReader(bufio.NewReader(&stdout))
	var docs []unstructured.Unstructured
	for {
		doc, err := r.Read()
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("split helm output: %w", err)
		}
		var u unstructured.Unstructured
		if err := sigsyaml.UnmarshalStrict(doc, &u.Object); err != nil {
			return nil, fmt.Errorf("decode helm output: %w\n%s", err, doc)
		}
		if len(u.Object) > 0 {
			docs = append(docs, u)
		}
	}
}

// mustRender is renderChart that fails the test on error.
func mustRender(t *testing.T, release, namespace string, extra ...string) []unstructured.Unstructured {
	t.Helper()
	docs, err := renderChart(t, release, namespace, extra...)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// findKind returns every document of the given kind.
func findKind(t *testing.T, docs []unstructured.Unstructured, kind string) []unstructured.Unstructured {
	t.Helper()
	var out []unstructured.Unstructured
	for _, d := range docs {
		if d.GetKind() == kind {
			out = append(out, d)
		}
	}
	return out
}

// onlyKind decodes the single document of the given kind into T, strictly, so a misspelled rendered field fails the test.
func onlyKind[T any](t *testing.T, docs []unstructured.Unstructured, kind string) T {
	t.Helper()
	got := findKind(t, docs, kind)
	if len(got) != 1 {
		t.Fatalf("want exactly one %s, got %d", kind, len(got))
	}
	var out T
	if err := runtime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(got[0].Object, &out, true); err != nil {
		t.Fatalf("decode %s: %v", kind, err)
	}
	return out
}

// pluginContainer returns the DaemonSet's only container.
func pluginContainer(t *testing.T, ds appsv1.DaemonSet) corev1.Container {
	t.Helper()
	if n := len(ds.Spec.Template.Spec.Containers); n != 1 {
		t.Fatalf("want one container, got %d", n)
	}
	return ds.Spec.Template.Spec.Containers[0]
}

func TestChartDaemonSetFixedFields(t *testing.T) {
	ds := onlyKind[appsv1.DaemonSet](t, mustRender(t, "usb-dra", "kube-system"), "DaemonSet")
	pod := ds.Spec.Template.Spec
	c := pluginContainer(t, ds)

	if sc := c.SecurityContext; sc == nil || sc.Privileged == nil || !*sc.Privileged {
		t.Errorf("container must be privileged, got %+v", sc)
	}
	if sc := c.SecurityContext; sc == nil || sc.RunAsUser == nil || *sc.RunAsUser != 0 {
		t.Errorf("container must run as uid 0, got %+v", sc)
	}
	if got := pod.AutomountServiceAccountToken; got == nil || !*got {
		t.Errorf("pod automountServiceAccountToken = %v, want true", got)
	}
	if got := pod.EnableServiceLinks; got == nil || *got {
		t.Errorf("pod enableServiceLinks = %v, want false", got)
	}
	for name, got := range map[string]bool{"hostNetwork": pod.HostNetwork, "hostPID": pod.HostPID, "hostIPC": pod.HostIPC} {
		if got {
			t.Errorf("pod %s must be false", name)
		}
	}

	if pod.PriorityClassName != "system-node-critical" {
		t.Errorf("priorityClassName = %q, want system-node-critical", pod.PriorityClassName)
	}
	if want := []corev1.Toleration{{Operator: corev1.TolerationOpExists}}; !reflect.DeepEqual(pod.Tolerations, want) {
		t.Errorf("tolerations = %+v, want %+v", pod.Tolerations, want)
	}

	wantArgs := []string{"--driver-name=usb.jacaudi.dev", "--rescan-interval=30s", "--exclude-classes=03,08,e0", "--cdi-root=/var/run/cdi", "-v=2"}
	if !slices.Equal(c.Args, wantArgs) {
		t.Errorf("args = %q, want %q", c.Args, wantArgs)
	}

	if len(c.Env) != 1 || c.Env[0].Name != "NODE_NAME" || c.Env[0].ValueFrom == nil || c.Env[0].ValueFrom.FieldRef == nil ||
		c.Env[0].ValueFrom.FieldRef.FieldPath != "spec.nodeName" {
		t.Errorf("env must be exactly NODE_NAME from fieldRef spec.nodeName, got %+v", c.Env)
	}

	wantVolumes := []struct {
		name, path string
		hostType   corev1.HostPathType
		readOnly   bool
	}{
		{"plugins-registry", "/var/lib/kubelet/plugins_registry", corev1.HostPathDirectory, false},
		{"plugins", "/var/lib/kubelet/plugins", corev1.HostPathDirectory, false},
		{"cdi", "/var/run/cdi", corev1.HostPathDirectoryOrCreate, false},
		{"sys", "/sys", corev1.HostPathDirectory, true},
	}
	if len(pod.Volumes) != len(wantVolumes) || len(c.VolumeMounts) != len(wantVolumes) {
		t.Fatalf("want %d volumes and mounts, got %d and %d", len(wantVolumes), len(pod.Volumes), len(c.VolumeMounts))
	}
	for _, w := range wantVolumes {
		vi := slices.IndexFunc(pod.Volumes, func(v corev1.Volume) bool { return v.Name == w.name })
		if vi < 0 {
			t.Errorf("volume %q missing", w.name)
			continue
		}
		if hp := pod.Volumes[vi].HostPath; hp == nil || hp.Path != w.path || hp.Type == nil || *hp.Type != w.hostType {
			t.Errorf("volume %q hostPath = %+v, want %s (%s)", w.name, hp, w.path, w.hostType)
		}
		mi := slices.IndexFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == w.name })
		if mi < 0 {
			t.Errorf("mount %q missing", w.name)
			continue
		}
		if m := c.VolumeMounts[mi]; m.MountPath != w.path || m.ReadOnly != w.readOnly {
			t.Errorf("mount %q = %s readOnly=%v, want %s readOnly=%v", w.name, m.MountPath, m.ReadOnly, w.path, w.readOnly)
		}
	}
}

func TestChartArgsFromValues(t *testing.T) {
	ds := onlyKind[appsv1.DaemonSet](t, mustRender(t, "usb-dra", "kube-system",
		"--set", "driver.include={0bda:2838,239a:8029:abc}", "--set", "driver.cdiRoot=/run/cdi"), "DaemonSet")
	c := pluginContainer(t, ds)

	wantArgs := []string{
		"--driver-name=usb.jacaudi.dev", "--rescan-interval=30s", "--exclude-classes=03,08,e0",
		"--include=0bda:2838", "--include=239a:8029:abc", "--cdi-root=/run/cdi", "-v=2",
	}
	if !slices.Equal(c.Args, wantArgs) {
		t.Errorf("args = %q, want %q", c.Args, wantArgs)
	}

	vi := slices.IndexFunc(ds.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool { return v.Name == "cdi" })
	mi := slices.IndexFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == "cdi" })
	if vi < 0 || mi < 0 {
		t.Fatalf("cdi volume (%d) or mount (%d) missing", vi, mi)
	}
	if hp := ds.Spec.Template.Spec.Volumes[vi].HostPath; hp == nil || hp.Path != "/run/cdi" {
		t.Errorf("cdi hostPath = %+v, want /run/cdi", hp)
	}
	if got := c.VolumeMounts[mi].MountPath; got != "/run/cdi" {
		t.Errorf("cdi mountPath = %q, want /run/cdi", got)
	}
}

// committedAppVersion reads appVersion from the committed Chart.yaml.
func committedAppVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	var chart struct {
		AppVersion string `json:"appVersion"`
	}
	if err := sigsyaml.Unmarshal(b, &chart); err != nil {
		t.Fatalf("decode Chart.yaml: %v", err)
	}
	return chart.AppVersion
}

// chartWithAppVersion copies the built chart to a temp dir with appVersion rewritten.
func chartWithAppVersion(t *testing.T, appVersion string) string {
	t.Helper()
	if err := buildChartDeps(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(chartDir)); err != nil {
		t.Fatalf("copy chart: %v", err)
	}
	p := filepath.Join(dir, "Chart.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read copied Chart.yaml: %v", err)
	}
	re := regexp.MustCompile(`(?m)^appVersion:.*$`)
	if !re.Match(b) {
		t.Fatal("Chart.yaml has no appVersion line")
	}
	b = re.ReplaceAll(b, fmt.Appendf(nil, "appVersion: %q", appVersion))
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write copied Chart.yaml: %v", err)
	}
	return dir
}

func TestChartImageTag(t *testing.T) {
	const repo = "ghcr.io/jacaudi/usb-dra:"
	tests := []struct {
		name       string
		appVersion string // "" renders the committed chart
		set        []string
		want       string
	}{
		{name: "committed appVersion, empty tag", set: []string{"--set", "image.tag="}, want: repo + "v" + strings.TrimPrefix(committedAppVersion(t), "v")},
		{name: "appVersion with v", appVersion: "v0.9.0", want: repo + "v0.9.0"},
		{name: "appVersion without v", appVersion: "0.9.0", want: repo + "v0.9.0"},
		{name: "explicit tag verbatim", set: []string{"--set", "image.tag=sha-abc"}, want: repo + "sha-abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var docs []unstructured.Unstructured
			if tt.appVersion == "" {
				docs = mustRender(t, "usb-dra", "kube-system", tt.set...)
			} else {
				var err error
				docs, err = renderChartAt(t, chartWithAppVersion(t, tt.appVersion), "usb-dra", "kube-system", tt.set...)
				if err != nil {
					t.Fatal(err)
				}
			}
			c := pluginContainer(t, onlyKind[appsv1.DaemonSet](t, docs, "DaemonSet"))
			if c.Image != tt.want {
				t.Errorf("image = %q, want %q", c.Image, tt.want)
			}
		})
	}
}

func TestChartRBAC(t *testing.T) {
	wantRules := []rbacv1.PolicyRule{
		{APIGroups: []string{"resource.k8s.io"}, Resources: []string{"resourceslices"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"resource.k8s.io"}, Resources: []string{"resourceclaims"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get"}},
	}
	for _, tt := range []struct{ release, namespace string }{
		{"usb-dra", "kube-system"},
		{"radios", "other-ns"},
	} {
		t.Run(tt.release+"/"+tt.namespace, func(t *testing.T) {
			docs := mustRender(t, tt.release, tt.namespace)
			role := onlyKind[rbacv1.ClusterRole](t, docs, "ClusterRole")
			binding := onlyKind[rbacv1.ClusterRoleBinding](t, docs, "ClusterRoleBinding")
			sa := onlyKind[corev1.ServiceAccount](t, docs, "ServiceAccount")

			if !reflect.DeepEqual(role.Rules, wantRules) {
				t.Errorf("ClusterRole rules = %+v, want %+v", role.Rules, wantRules)
			}
			if binding.RoleRef.Kind != "ClusterRole" || binding.RoleRef.Name != role.Name {
				t.Errorf("roleRef = %+v, want ClusterRole %q", binding.RoleRef, role.Name)
			}
			if sa.Namespace != tt.namespace {
				t.Errorf("ServiceAccount namespace = %q, want %q", sa.Namespace, tt.namespace)
			}
			wantSubject := rbacv1.Subject{Kind: "ServiceAccount", Name: sa.Name, Namespace: tt.namespace}
			if len(binding.Subjects) != 1 || binding.Subjects[0] != wantSubject {
				t.Errorf("binding subjects = %+v, want [%+v]", binding.Subjects, wantSubject)
			}
		})
	}
}

func TestChartSchemaRejects(t *testing.T) {
	for _, set := range []string{
		"driver.excludeClass=03",
		"fullnameOverride=x",
		"nameOverride=x",
		"global.fullnameOverride=x",
		"global.alwaysAppendIdentifierToResourceName=true",
		"image.pullPolicy=Sometimes",
		"driver.cdiRoot=/a: b",
		"driver.cdiRoot=/run/cdi #x",
	} {
		t.Run(set, func(t *testing.T) {
			if _, err := renderChart(t, "usb-dra", "kube-system", "--set", set); err == nil {
				t.Errorf("--set %s rendered, want a schema error", set)
			}
		})
	}
}

// TestChartCDIRootMovesTogether guards the Global Constraint that --cdi-root, the cdi hostPath and its mountPath are one value, and that moving it never drops another host mount.
func TestChartCDIRootMovesTogether(t *testing.T) {
	const root = "/run/cdi-x.y_z"
	ds := onlyKind[appsv1.DaemonSet](t, mustRender(t, "usb-dra", "kube-system", "--set", "driver.cdiRoot="+root), "DaemonSet")
	pod := ds.Spec.Template.Spec
	c := pluginContainer(t, ds)

	if len(pod.Volumes) != 4 || len(c.VolumeMounts) != 4 {
		t.Fatalf("want 4 volumes and 4 mounts, got %d and %d", len(pod.Volumes), len(c.VolumeMounts))
	}
	if want := "--cdi-root=" + root; !slices.Contains(c.Args, want) {
		t.Errorf("args = %q, want %q", c.Args, want)
	}
	vi := slices.IndexFunc(pod.Volumes, func(v corev1.Volume) bool { return v.Name == "cdi" })
	mi := slices.IndexFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == "cdi" })
	if vi < 0 || mi < 0 {
		t.Fatalf("cdi volume (%d) or mount (%d) missing", vi, mi)
	}
	if hp := pod.Volumes[vi].HostPath; hp == nil || hp.Path != root {
		t.Errorf("cdi hostPath = %+v, want %s", hp, root)
	}
	if got := c.VolumeMounts[mi].MountPath; got != root {
		t.Errorf("cdi mountPath = %q, want %s", got, root)
	}
}
