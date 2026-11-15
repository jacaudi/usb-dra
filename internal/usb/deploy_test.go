package usb

import (
	"slices"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

// TestSlicePolicyScopesDriverAccount guards the node boundary: RBAC cannot scope resourceslices writes by node, so the shipped admission policy must match the DaemonSet's own service account, deny on failure and pin the driver name.
func TestSlicePolicyScopesDriverAccount(t *testing.T) {
	var (
		user    string
		policy  *admissionv1.ValidatingAdmissionPolicy
		binding *admissionv1.ValidatingAdmissionPolicyBinding
	)
	for _, doc := range readDocs(t, "../../deploy/daemonset.yaml") {
		var tm metav1.TypeMeta
		if err := sigsyaml.Unmarshal(doc, &tm); err != nil {
			t.Fatalf("decode kind: %v", err)
		}
		var err error
		switch tm.Kind {
		case "ServiceAccount":
			var sa corev1.ServiceAccount
			err = sigsyaml.UnmarshalStrict(doc, &sa)
			user = "system:serviceaccount:" + sa.Namespace + ":" + sa.Name
		case "ValidatingAdmissionPolicy":
			policy = &admissionv1.ValidatingAdmissionPolicy{}
			err = sigsyaml.UnmarshalStrict(doc, policy)
		case "ValidatingAdmissionPolicyBinding":
			binding = &admissionv1.ValidatingAdmissionPolicyBinding{}
			err = sigsyaml.UnmarshalStrict(doc, binding)
		}
		if err != nil {
			t.Fatalf("decode %s: %v", tm.Kind, err)
		}
	}
	if policy == nil || binding == nil {
		t.Fatalf("deploy/daemonset.yaml ships no ValidatingAdmissionPolicy and binding for resourceslices")
	}
	if binding.Spec.PolicyName != policy.Name || !slices.Contains(binding.Spec.ValidationActions, admissionv1.Deny) {
		t.Errorf("binding %q must reference policy %q with action Deny, got %q %v", binding.Name, policy.Name, binding.Spec.PolicyName, binding.Spec.ValidationActions)
	}
	if fp := policy.Spec.FailurePolicy; fp == nil || *fp != admissionv1.Fail {
		t.Errorf("policy failurePolicy must be Fail, got %v", fp)
	}
	wantMatch := `request.userInfo.username == "` + user + `"`
	if !slices.ContainsFunc(policy.Spec.MatchConditions, func(m admissionv1.MatchCondition) bool { return m.Expression == wantMatch }) {
		t.Errorf("policy must match the DaemonSet's service account with %s", wantMatch)
	}
	if !slices.ContainsFunc(policy.Spec.Validations, func(v admissionv1.Validation) bool { return strings.Contains(v.Expression, "'"+DriverName+"'") }) {
		t.Errorf("no policy validation pins the driver name %q", DriverName)
	}
}
