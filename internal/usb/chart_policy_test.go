package usb

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

// TestSlicePolicyScopesDriverAccount guards the node boundary: RBAC cannot scope resourceslices writes by node, so the rendered admission policy must match the service account the chart creates (under any release name and namespace), deny on failure and pin the driver name.
func TestSlicePolicyScopesDriverAccount(t *testing.T) {
	wantConstraints := &admissionv1.MatchResources{
		ResourceRules: []admissionv1.NamedRuleWithOperations{{
			RuleWithOperations: admissionv1.RuleWithOperations{
				Operations: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete},
				Rule:       admissionv1.Rule{APIGroups: []string{"resource.k8s.io"}, APIVersions: []string{"*"}, Resources: []string{"resourceslices"}},
			},
		}},
	}
	for _, tt := range []struct{ release, namespace string }{
		{"usb-dra", "kube-system"},
		{"dra", "kube-system"},
		{"usb-dra", "drivers"},
	} {
		t.Run(tt.release+"/"+tt.namespace, func(t *testing.T) {
			docs := mustRender(t, tt.release, tt.namespace)
			sa := onlyKind[corev1.ServiceAccount](t, docs, "ServiceAccount")
			crb := onlyKind[rbacv1.ClusterRoleBinding](t, docs, "ClusterRoleBinding")
			ds := onlyKind[appsv1.DaemonSet](t, docs, "DaemonSet")
			policy := onlyKind[admissionv1.ValidatingAdmissionPolicy](t, docs, "ValidatingAdmissionPolicy")
			binding := onlyKind[admissionv1.ValidatingAdmissionPolicyBinding](t, docs, "ValidatingAdmissionPolicyBinding")

			user := "system:serviceaccount:" + tt.namespace + ":" + sa.Name

			if binding.Spec.PolicyName != policy.Name || !slices.Contains(binding.Spec.ValidationActions, admissionv1.Deny) {
				t.Errorf("binding %q must reference policy %q with action Deny, got %q %v", binding.Name, policy.Name, binding.Spec.PolicyName, binding.Spec.ValidationActions)
			}
			if fp := policy.Spec.FailurePolicy; fp == nil || *fp != admissionv1.Fail {
				t.Errorf("policy failurePolicy must be Fail, got %v", fp)
			}
			if !reflect.DeepEqual(policy.Spec.MatchConstraints, wantConstraints) {
				t.Errorf("policy matchConstraints = %+v, want %+v", policy.Spec.MatchConstraints, wantConstraints)
			}

			wantMatch := `request.userInfo.username == "` + user + `"`
			if !slices.ContainsFunc(policy.Spec.MatchConditions, func(m admissionv1.MatchCondition) bool { return m.Expression == wantMatch }) {
				t.Errorf("policy matchConditions = %+v, want one with expression %s", policy.Spec.MatchConditions, wantMatch)
			}
			wantSubject := rbacv1.Subject{Kind: "ServiceAccount", Name: sa.Name, Namespace: tt.namespace}
			if len(crb.Subjects) != 1 || crb.Subjects[0] != wantSubject {
				t.Errorf("ClusterRoleBinding subjects = %+v, want [%+v]", crb.Subjects, wantSubject)
			}
			if got := ds.Spec.Template.Spec.ServiceAccountName; got != sa.Name {
				t.Errorf("DaemonSet serviceAccountName = %q, want %q", got, sa.Name)
			}

			hasValidation := func(substr string) bool {
				return slices.ContainsFunc(policy.Spec.Validations, func(v admissionv1.Validation) bool { return strings.Contains(v.Expression, substr) })
			}
			for _, substr := range []string{"'" + DriverName + "'", "variables.userNodeName != ''", "nodeName.orValue('') == variables.userNodeName"} {
				if !hasValidation(substr) {
					t.Errorf("no policy validation contains %q", substr)
				}
			}
			if !slices.ContainsFunc(policy.Spec.Variables, func(v admissionv1.Variable) bool { return strings.Contains(v.Expression, "[object, oldObject]") }) {
				t.Errorf("no policy variable checks both [object, oldObject]: %+v", policy.Spec.Variables)
			}
		})
	}
}
