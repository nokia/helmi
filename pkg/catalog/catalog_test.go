package catalog

import (
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"testing"
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
---
chart-values:
    foo: "bar"
    password: "{{ generatePassword }}"
---
user-credentials:
    key: "{{ .Values.foo }}"
    plan_key: "{{ .Values.baz }}"
    addr: "{{ .Cluster.Address }}"
    hostname: "{{ .Cluster.Hostname }}"
    port: "{{ .Cluster.Port }}"
    translated_port: "{{ .Cluster.Port 80 }}"
    fallback_port: "{{ .Cluster.Port 8080 }}"
    namespace: "{{ .Release.Namespace }}"
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

	if values["foo"] != "bar" {
		t.Error(red("incorrect helm value returned"))
	}
	if len(values["password"]) != 32 {
		t.Error(red("incorrect helm value returned"))
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

	credentials, err := s.UserCredentials(p, nodes, status, values)
	if err != nil {
		t.Error(red(err.Error()))
	}

	if credentials["key"] != "bar" {
		t.Error(red("incorrect lookup value returned"))
	}
	if credentials["plan_key"] != "qux" {
		t.Error(red("incorrect lookup value returned"))
	}
	if credentials["addr"] != "2.2.2.2" {
		t.Error(red("incorrect address value returned"))
	}
	if credentials["hostname"] != "test_hostname" {
		t.Error(red("incorrect hostname value returned"))
	}
	if credentials["port"] != "30001" {
		t.Error(red("incorrect port value returned"))
	}
	if credentials["translated_port"] != "30001" {
		t.Error(red("incorrect port value returned"))
	}
	if credentials["fallback_port"] != "8080" {
		t.Error(red("incorrect port value returned"))
	}
	if credentials["namespace"] != "test_namespace" {
		t.Error(red("incorrect release value returned"))
	}
}
