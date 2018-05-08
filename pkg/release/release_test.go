package release

import (
	"testing"
	"github.com/monostream/helmi/pkg/catalog"
)

var csp = catalog.Plan{
	Id:          "67890",
	Name:        "test_plan",
	Description: "plan_description",

	Chart: "plan_chart",
	ChartVersion: "1.2.3",
}
var cs = catalog.Service{
	Id:          "12345",
	Name:        "test_service",
	Description: "service_description",

	Chart: "service_chart",
	ChartVersion: "1.2.3",

	Plans: []catalog.Plan{
		csp,
	},
}

func red(msg string) (string){
	return "\033[31m" + msg + "\033[39m\n\n"
}

func Test_GetName(t *testing.T) {
	const input string = "this_is-a_test_name_which-is_pretty-long"
	const expected string = "helmithisisatestnam"

	name := getName(input)

	if len(name) != len(expected) {
		t.Error(red("length is wrong"))
	}
	if name != expected {
		t.Error(red("name is wrong"))
	}
}

func Test_GetChart(t *testing.T) {
	chart, _ := getChart(&cs, &catalog.Plan{})
	if chart != "service_chart" {
		t.Error(red("service chart not returned"))
	}
	chart, _ = getChart(&cs, &cs.Plans[0])
	if chart != "plan_chart" {
		t.Error(red("plan chart not returned"))
	}
	// no chart in plan
	csp.Chart = ""
	chart, _ = getChart(&cs, &csp)
	if chart != "service_chart" {
		t.Error(red("service chart for empty plan not returned"))
	}
}

func Test_GetChartVersion(t *testing.T) {
	version, _ := getChartVersion(&cs, &csp)

	if version != "1.2.3" {
		t.Error(red("incorrect chart version returned"))
	}
}
