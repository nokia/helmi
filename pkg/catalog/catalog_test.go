package catalog

import (
	"fmt"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"reflect"
	"testing"
)

var def = []byte(`---
service:
  _id: 12345
  _name: "test_service"
  description: "service_description"
  metadata:
    somekey: somevalue
  chart: service_chart
  chart-version: 1.2.3
  plans:
  -
    _id: 67890
    _name: test_plan
    description: "plan_description"
    metadata:
      someplankey: someplanvalue
    chart: "plan_chart"
    chart-version: "4.5.6"
    chart-values:
      baz: qux
      nested:
        from_plan: "from plan"
---
chart-values:
    foo: "bar"
    username: "{{ generateUsername }}"
    password: "{{ generatePassword }}"
    hostname: "{{ .Cluster.IngressDomain }}"
    nested:
      from_vals: "from vals"
---
user-credentials:
    key: "{{ .Values.foo }}"
    plan_key: "{{ .Values.baz }}"
    cluster_addr: "{{ .Cluster.Address }}"
    cluster_hostname: "{{ .Cluster.Hostname }}"
    port: {{ .Services.Port "test_service" 7070 }}
    node_port: {{ .Services.Port "node_service" 8080 }}
    lb_port: {{ .Services.Port "lb_service" 9090 }}
    any_port: {{ .Services.FindPort 8080 }}
    addr: "{{ .Services.Address "test_service" 7070 }}"
    lb_addr: "{{ .Services.Address "lb_service" 9090 }}"
    node_addr: "{{ .Services.Address "node_service" 8080 }}"
    namespace: "{{ .Release.Namespace }}"
    nested:
{{ toYaml .Values.nested | indent 8 }}
`)

var defNoMetadata = []byte(`---
service:
  _id: 12345
  _name: "test_service"
  description: "service_description"
  chart: service_chart
  chart-version: 1.2.3
  plans:
  -
    _id: 67890
    _name: test_plan
    description: "plan_description"
    chart: "plan_chart"
    chart-version: "4.5.6"
    chart-values:
      baz: qux
      nested:
        from_plan: "from plan"
---
chart-values:
    foo: "bar"
    username: "{{ generateUsername }}"
    password: "{{ generatePassword }}"
    hostname: "{{ .Cluster.IngressDomain }}"
    nested:
      from_vals: "from vals"
---
user-credentials:
    key: "{{ .Values.foo }}"
    plan_key: "{{ .Values.baz }}"
    cluster_addr: "{{ .Cluster.Address }}"
    cluster_hostname: "{{ .Cluster.Hostname }}"
    port: {{ .Services.Port "test_service" 7070 }}
    node_port: {{ .Services.Port "node_service" 8080 }}
    lb_port: {{ .Services.Port "lb_service" 9090 }}
    any_port: {{ .Services.FindPort 8080 }}
    addr: "{{ .Services.Address "test_service" 7070 }}"
    lb_addr: "{{ .Services.Address "lb_service" 9090 }}"
    node_addr: "{{ .Services.Address "node_service" 8080 }}"
    namespace: "{{ .Release.Namespace }}"
    nested:
{{ toYaml .Values.nested | indent 8 }}
`)

var nodes = []kubectl.Node{
	{
		Name: "test_node",

		Hostname:   "test_hostname",
		InternalIP: "1.1.1.1",
		ExternalIP: "2.2.2.2",
	},
}

var status = helm.Status{
	Name:       "test_release",
	Namespace:  "test_namespace",
	IsFailed:   false,
	IsDeployed: true,

	DesiredNodes:    1,
	AvailableNodes:  1,

	Services: map[string]kubectl.Service{
		"test_service": {
			Type:         "ClusterIP",
			ClusterPorts: map[int]int{7070: 7070},
			ClusterIP:    "10.0.70.70",
		},
		"node_service": {
			Type:      "NodePort",
			NodePorts: map[int]int{8080: 31008},
			ClusterIP: "10.0.80.80",
		},
		"lb_service": {
			Type:       "LoadBalancer",
			NodePorts:  map[int]int{9090: 31009},
			ExternalIP: "3.3.3.3",
		},
	},
}

func red(msg string) string {
	return "\033[31m" + msg + "\033[39m\n\n"
}

func Test_CatalogDir(t *testing.T) {
	_, err := parseDir("../../catalog")
	if err != nil {
		t.Error(red(err.Error()))
	}
}

func getCatalog(t *testing.T) Catalog {
	return deserializeCatalog(t, def)
}

func getCatalogWithoutMetadata(t *testing.T) Catalog {
	return deserializeCatalog(t, defNoMetadata)
}

func deserializeCatalog(t *testing.T, serializedCatalog []byte) Catalog {
	catalog, err := NewFromSerialized(serializedCatalog)

	if err != nil {
		t.Error(red(err.Error()))
	}

	return *catalog
}


func Test_GetService(t *testing.T) {
	c := getCatalog(t)
	cs := c.Service("12345")
	if cs.Name != "test_service" {
		t.Error(red("service name is wrong"))
	}

	if cs.Metadata["somekey"] != "somevalue" {
		t.Error(red("metadata does not contain 'somekey' with value 'somevalue'"))
	}
}

func Test_GetService_NoMetadata(t *testing.T) {
	c := getCatalogWithoutMetadata(t)
	cs := c.Service("12345")

	if cs.Metadata["somekey"] != nil {
		t.Error(red("metadata should not contain 'somekey'"))
	}
}

func Test_GetServicePlan(t *testing.T) {
	c := getCatalog(t)
	csp := c.Service("12345").Plan("67890")

	if csp.Name != "test_plan" {
		t.Error(red("service plan is wrong"))
	}
	if csp.ChartValues["baz"] != "qux" {
		t.Error(red("chart value in plan is wrong"))
	}

	if csp.Metadata["someplankey"] != "someplanvalue" {
		t.Error(red("metadata does not contain 'someplankey' with value 'someplanvalue'"))
	}
}

func Test_GetServicePlan_NoMetadata(t *testing.T) {
	c := getCatalogWithoutMetadata(t)
	csp := c.Service("12345").Plan("67890")

	if csp.Name != "test_plan" {
		t.Error(red("service plan is wrong"))
	}
	if csp.ChartValues["baz"] != "qux" {
		t.Error(red("chart value in plan is wrong"))
	}

	if csp.Metadata["someplankey"] != nil {
		t.Error(red("metadata should not contain 'someplankey'"))
	}
}


func Test_GetChartValues(t *testing.T) {
	ns := kubectl.Namespace{
		Name:          "testnamespace",
		IngressDomain: "test.ingress.domain",
	}

	c := getCatalog(t)
	s := c.Service("12345")
	p := s.Plan("67890")
	values, err := s.ChartValues(p, "RELEASE-NAME", ns, nil)
	if err != nil {
		t.Error(red(err.Error()))
	}

	expected := map[string]interface{}{
		"foo":      "bar",
		"baz":      "qux",
		"username": values["username"], // cheat
		"password": values["password"], // cheat
		"hostname": "test.ingress.domain",
		"nested": map[string]interface{}{
			"from_plan": "from plan",
			"from_vals": "from vals",
		},
		metadataKey: map[string]interface{}{
			metadataServiceIdKey:  s.Id,
			metadataPlanIdKey:     p.Id,
			metadataIngressDomain: ns.IngressDomain,
		},
	}

	if !reflect.DeepEqual(expected, values) {
		t.Error(red(fmt.Sprintf("expected %v, got  %v", expected, values)))
	}
}

func Test_GetUserCredentials(t *testing.T) {
	ns := kubectl.Namespace{
		Name:          "testnamespace",
		IngressDomain: "test.ingress.domain",
	}

	c := getCatalog(t)
	s := c.Service("12345")
	p := s.Plan("67890")

	values, err := s.ChartValues(p, "RELEASE-NAME", ns, nil)
	if err != nil {
		t.Error(red(err.Error()))
	}

	release, err := s.ReleaseSection(p, nodes, status, values)
	if err != nil {
		t.Error(red(err.Error()))
	}

	expected := map[string]interface{}{
		"key":              values["foo"],
		"plan_key":         values["baz"],
		"cluster_hostname": nodes[0].Hostname,
		"cluster_addr":     nodes[0].ExternalIP,
		"port":             7070,
		"node_port":        31008,
		"lb_port":          9090,
		"any_port":         31008,
		"addr":             "10.0.70.70:7070",
		"lb_addr":          "3.3.3.3:9090",
		"node_addr":        "2.2.2.2:31008",
		"namespace":        status.Namespace,
		"nested": map[string]interface{}{
			"from_plan": "from plan",
			"from_vals": "from vals",
		},
	}

	credentials := release.UserCredentials
	if !reflect.DeepEqual(expected, credentials) {
		t.Error(red(fmt.Sprintf("expected %#v, got  %#v", expected, credentials)))
	}
}

func Test_mergeMaps(t *testing.T) {
	a := map[string]interface{}{
		"a": 1,
		"b": 2,
		"bothmap": map[string]interface{}{
			"five":        5,
			"overwritten": nil,
		},
		"srcmap": 0.0,
	}

	b := map[string]interface{}{
		"a": 3,
		"c": 4,
		"bothmap": map[string]interface{}{
			"six":         6,
			"overwritten": true,
		},
		"srcmap": map[string]interface{}{
			"seven": 7,
		},
	}

	got := mergeMaps(a, b)

	expected := map[string]interface{}{
		"a": 3,
		"b": 2,
		"c": 4,
		"bothmap": map[string]interface{}{
			"five":        5,
			"six":         6,
			"overwritten": true,
		},
		"srcmap": map[string]interface{}{
			"seven": 7,
		},
	}

	if !reflect.DeepEqual(expected, got) {
		t.Error(red(fmt.Sprintf("expected %v, got  %v", expected, got)))
	}
}
