package broker

import (
	"github.com/monostream/helmi/pkg/catalog"
	"testing"
)

var def = []byte(`---
service:
  _id: 12345
  _name: "test_service"
  description: "service_description"
  metadata:
    somekey: somevalue
  tags:
  - testtag
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

func red(msg string) string {
	return "\033[31m" + msg + "\033[39m\n\n"
}

func Test_Services_Metadata(t *testing.T) {
	catalog, err := catalog.NewFromSerialized(def)

	if err != nil {
		t.Error(red(err.Error()))
	}

	broker := NewBroker(catalog, Config{}, nil)

	services, err := broker.Services(nil)

	if err != nil {
		t.Error(red(err.Error()))
	}

	if services[0].Metadata.AdditionalMetadata["somekey"] != "somevalue" {
		t.Error(red("metadata does not contain 'somekey' with value 'somevalue'"))
	}

	if services[0].Plans[0].Metadata.AdditionalMetadata["someplankey"] != "someplanvalue" {
		t.Error(red("metadata does not contain 'someplankey' with value 'someplanvalue'"))
	}

	if services[0].Tags[0] != "testtag" {
		t.Error(red("tags does not contain 'testtag'"))
	}
}

func Test_Services_NoMetadata(t *testing.T) {
	catalog, err := catalog.NewFromSerialized(defNoMetadata)

	if err != nil {
		t.Error(red(err.Error()))
	}

	broker := NewBroker(catalog, Config{}, nil)

	services, err := broker.Services(nil)

	if err != nil {
		t.Error(red(err.Error()))
	}

	if services[0].Metadata.AdditionalMetadata["somekey"] != nil {
		t.Error(red("metadata should not contain 'somekey'"))
	}

	if services[0].Plans[0].Metadata.AdditionalMetadata["someplankey"] != nil {
		t.Error(red("metadata should not contain 'someplankey'"))
	}
}
