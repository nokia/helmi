package release

import (
	"testing"

	"github.com/monostream/helmi/pkg/catalog"
)

var csp = catalog.Plan{
	Id:          "67890",
	Name:        "test_plan",
	Description: "plan_description",

	Chart:        "plan_chart",
	ChartVersion: "1.2.3",
}
var cs = catalog.Service{
	Id:          "12345",
	Name:        "test_service",
	Description: "service_description",

	Chart:        "service_chart",
	ChartVersion: "1.2.3",

	Plans: []catalog.Plan{
		csp,
	},
}

func red(msg string) string {
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

func Test_Healthchecks(t *testing.T) {
	// url -> shouldSucceed
	healthChecks := map[string]bool{
		"http://httpbin.org/status/200":                                              true,
		"http://httpbin.org/status/302":                                              true,
		"http://httpbin.org/status/404":                                              false,
		"http://httpbin.org/status/500":                                              false,
		"http://httpbin.org/absolute-redirect/1":                                     true,
		"http://httpbin.org/absolute-redirect/100":                                   false,
		"http://httpbin.org/redirect-to?url=http%3A%2F%2Fhttpbin.org%2Fstatus%2F404": false,
		"http://user:pass@httpbin.org/basic-auth/user/pass":                          true,
		"http://user:wrong@httpbin.org/basic-auth/user/pass":                         false,

		"https://httpbin.org/status/200":         true,
		"https://httpbin.org/status/500":         false,
		"https://extended-validation.badssl.com": true,
		"https://wrong.host.badssl.com":          false,
		"https://self-signed.badssl.com":         false,
		"https://untrusted-root.badssl.com":      false,

		"tcp://8.8.8.8:53":   true,
		"tcp://localhost:1":  false,
		"tcp://missing-port": false,

		"tls://badssl.com:443":             true,
		"tls://expired.badssl.com:443":     false,
		"tls://self-signed.badssl.com:443": false,
		"tls://wrong.host.badssl.com:443":  false,
	}

	for endpoint, shouldSucceed := range healthChecks {
		err := checkHealth(endpoint)
		if shouldSucceed {
			if err != nil {
				t.Errorf(red("health check %s failed unexpectedly: %s"), endpoint, err)
			}
		} else {
			if err == nil {
				t.Errorf(red("health check %s succeeded unexpectedly"), endpoint)
			}
		}
	}
}