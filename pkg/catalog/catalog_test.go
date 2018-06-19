package catalog

import (
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"testing"
	"reflect"
	"fmt"
)

var def = []byte(`---
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
    password: "{{ generatePassword }}"
    nested:
      from_vals: "from vals"
---
user-credentials:
    key: "{{ .Values.foo }}"
    plan_key: "{{ .Values.baz }}"
    addr: "{{ .Cluster.Address }}"
    hostname: "{{ .Cluster.Hostname }}"
    port: {{ .Cluster.Port }}
    translated_port: {{ .Cluster.Port 80 }}
    fallback_port: {{ .Cluster.Port 8080 }}
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

	DesiredNodes:   1,
	AvailableNodes: 1,

	NodePorts: map[int]int{
		80: 30001,
	},
}

func red(msg string) string {
	return "\033[31m" + msg + "\033[39m\n\n"
}

func Test_CatalogDir(t *testing.T) {
	_, err := ParseDir("../../catalog")
	if err != nil {
		t.Error(red(err.Error()))
	}
}

func getCatalog(t *testing.T) Catalog {
	c := Catalog{
		Services: make(map[string]Service),
	}
	err := c.parseServiceDefinition(def, "<no file>")
	if err != nil {
		t.Error(red(err.Error()))
	}

	return c
}

func Test_GetService(t *testing.T) {
	c := getCatalog(t)
	cs := c.Service("12345")
	if cs.Name != "test_service" {
		t.Error(red("service name is wrong"))
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
}

func Test_GetChartValues(t *testing.T) {
	c := getCatalog(t)
	s := c.Service("12345")
	p := s.Plan("67890")
	values, err := s.ChartValues(p)
	if err != nil {
		t.Error(red(err.Error()))
	}

	expected := map[string]interface{}{
		"foo": "bar",
		"baz": "qux",
		"password": values["password"], // cheat
		"nested": map[string]interface{} {
			"from_plan": "from plan",
			"from_vals": "from vals",
		},
	}

	if !reflect.DeepEqual(expected, values) {
		t.Error(red(fmt.Sprintf("expected %v, got  %v", expected, values)))
	}
}

func Test_GetUserCredentials(t *testing.T) {
	c := getCatalog(t)
	s := c.Service("12345")
	p := s.Plan("67890")

	values, err := s.ChartValues(p)
	if err != nil {
		t.Error(red(err.Error()))
	}

	secrets := make(map[string]interface {})
	credentials, err := s.UserCredentials(p, nodes, status, values, secrets)
	if err != nil {
		t.Error(red(err.Error()))
	}

	// TODO(swicki): re-evaluate if this behavior really make sense
	expected_port := 0
	for _, p := range status.NodePorts {
		// extract first port
		expected_port = p
		break
	}

	expected := map[string]interface{}{
		"key": values["foo"],
		"plan_key": values["baz"],
		"hostname": nodes[0].Hostname,
		"addr": nodes[0].ExternalIP,
		"port": expected_port,
		"translated_port": status.NodePorts[80],
		"fallback_port": 8080,
		"namespace": status.Namespace,
		"nested": map[string]interface{} {
			"from_plan": "from plan",
			"from_vals": "from vals",
		},
	}

	if !reflect.DeepEqual(expected, credentials) {
		t.Error(red(fmt.Sprintf("expected %#v, got  %#v", expected, credentials)))
	}
}

func Test_mergeMaps(t *testing.T) {
	a := map[string]interface{}{
		"a" : 1,
		"b" : 2,
		"bothmap": map[string]interface{} {
			"five": 5,
			"overwritten": nil,
		},
		"srcmap": 0.0,
	}

	b := map[string]interface{}{
		"a" : 3,
		"c" : 4,
		"bothmap": map[string]interface{} {
			"six": 6,
			"overwritten": true,
		},
		"srcmap": map[string]interface{} {
			"seven": 7,
		},
	}

	got := mergeMaps(a, b)

	expected := map[string]interface{}{
		"a" : 3,
		"b" : 2,
		"c" : 4,
		"bothmap": map[string]interface{} {
			"five": 5,
			"six": 6,
			"overwritten": true,
		},
		"srcmap": map[string]interface{} {
			"seven": 7,
		},
	}

	if !reflect.DeepEqual(expected, got) {
		t.Error(red(fmt.Sprintf("expected %v, got  %v", expected, got)))
	}
}
