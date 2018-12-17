package helm

import (
	"bufio"
	"bytes"
	"errors"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

type Chart struct {
	Name        string
	Description string

	AppVersion   string
	ChartVersion string
}

func ListCharts() (map[string]Chart, error) {
	cmd := exec.Command("helm", "search")
	output, err := cmd.CombinedOutput()

	if err != nil {
		return nil, err
	}

	charts := make(map[string]Chart)

	scanner := bufio.NewScanner(bytes.NewReader(output))

	const NameLabel = "NAME"
	const DescriptionLabel = "DESCRIPTION"
	const AppVersionLabel = "APP VERSION"
	const ChartVersionLabel = "CHART VERSION"

	columnName := -1
	columnDescription := -1
	columnAppVersion := -1
	columnChartVersion := -1

	for scanner.Scan() {
		line := scanner.Text()

		indexName := strings.Index(line, NameLabel)
		indexDescription := strings.Index(line, DescriptionLabel)
		indexAppVersion := strings.Index(line, AppVersionLabel)
		indexChartVersion := strings.Index(line, ChartVersionLabel)

		if indexName >= 0 && indexDescription >= 0 && indexAppVersion >= 0 && indexChartVersion >= 0 {
			columnName = indexName
			columnDescription = indexDescription

			columnAppVersion = indexAppVersion
			columnChartVersion = indexChartVersion
		} else {
			if columnName >= 0 && columnDescription >= 0 && columnAppVersion >= 0 && columnChartVersion >= 0 {
				name := strings.Fields(line[columnName:])[0]
				description := strings.Fields(line[columnDescription:])[0]
				appVersion := strings.Fields(line[columnAppVersion:])[0]
				chartVersion := strings.Fields(line[columnChartVersion:])[0]

				chart := Chart{
					Name:        name,
					Description: description,

					AppVersion:   appVersion,
					ChartVersion: chartVersion,
				}

				charts[chart.Name] = chart
			}
		}
	}

	return charts, nil
}

type Service struct {
	Type         string
	NodePorts    map[int]int
	ClusterPorts map[int]int
	ExternalIP   string
	ClusterIP    string
}

func (svc *Service) PortMapping(port int) (int, bool) {
	switch svc.Type {
	case "NodePort":
		if nodePort, ok := svc.NodePorts[port]; ok {
			// return the mapped port
			return nodePort, true
		}
	case "LoadBalancer":
		if _, ok := svc.NodePorts[port]; ok {
			// no mapping needed
			return port, true
		}
	case "ClusterIP":
		if clusterPort, ok := svc.ClusterPorts[port]; ok {
			// return the mapped port
			return clusterPort, true
		}
	}

	return 0, false
}

type Status struct {
	Name       string
	Namespace  string
	IsFailed   bool
	IsDeployed bool

	DesiredNodes   int
	AvailableNodes int

	PendingServices int
	Services        map[string]*Service
	DeploymentTime  time.Time
}

func (s *Status) IsAvailable() bool {
	return !s.IsFailed &&
		s.IsDeployed &&
		s.AvailableNodes >= s.DesiredNodes &&
		s.PendingServices == 0
}

func Exists(release string) (bool, error) {
	cmd := exec.Command("helm", "status", release)
	output, err := cmd.CombinedOutput()

	if err == nil && len(output) > 0 {
		return true, nil
	}

	if output != nil && len(output) > 0 {
		text := string(output)

		if strings.Contains(strings.ToLower(text), "not found") {
			return false, nil
		}
	}

	return false, err
}

func Install(release string, chart string, version string, values map[string]interface{}, namespace string, acceptsIncomplete bool) error {
	arguments := make([]string, 0)

	arguments = append(arguments, "install", chart)
	arguments = append(arguments, "--name", release)

	if len(namespace) > 0 {
		arguments = append(arguments, "--namespace", namespace)
	}

	if len(version) > 0 {
		arguments = append(arguments, "--version", version)
	}

	if acceptsIncomplete == false {
		arguments = append(arguments, "--wait")
	}

	if len(values) > 0 {
		arguments = append(arguments, "--values", "-")
	}

	cmd := exec.Command("helm", arguments...)
	if len(values) > 0 {
		// pass values as yaml on stdin
		buf, err := yaml.Marshal(values)
		if err != nil {
			return err
		}
		cmd.Stdin = bytes.NewReader(buf)
	}

	output, err := cmd.CombinedOutput()

	if err != nil {
		return errors.New(string(output[:]))
	}

	return nil
}

func Delete(release string) error {
	cmd := exec.Command("helm", "delete", release, "--purge")
	output, err := cmd.CombinedOutput()

	if err != nil {
		return errors.New(string(output[:]))
	}

	return nil
}

func GetValues(release string) (map[string]interface{}, error) {
	cmd := exec.Command("helm", "get", "values", release, "--all")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var values map[string]interface{}
	err = yaml.Unmarshal(output, &values)
	if err != nil {
		return nil, err
	}

	return values, err
}

func GetStatus(release string) (Status, error) {
	cmd := exec.Command("helm", "status", release)
	output, err := cmd.CombinedOutput()

	status := Status{
		DesiredNodes:   0,
		AvailableNodes: 0,

		Services: make(map[string]*Service),
	}

	if err != nil {
		return status, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))

	const StatusFailed = "STATUS: FAILED"
	const StatusDeployed = "STATUS: DEPLOYED"

	const ResourcePrefix = "==> "

	const NamespacePrefix = "NAMESPACE: "
	const DeploymentTimePrefix = "LAST DEPLOYED: "

	const DesiredLabel = "DESIRED"
	const CurrentLabel = "CURRENT"
	const AvailableLabel = "AVAILABLE"

	const NameLabel = "NAME"
	const TypeLabel = "TYPE"
	const ClusterIPLabel = "CLUSTER-IP"
	const ExternalIPLabel = "EXTERNAL-IP"
	const PortsLabel = "PORT(S)"

	var lastResource string
	var lastDeploymentTime time.Time

	columnDesired := -1
	columnCurrent := -1
	columnAvailable := -1

	columnName := -1
	columnType := -1
	columnClusterIP := -1
	columnExternalIP := -1
	columnPort := -1

	// our name
	status.Name = release

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, StatusFailed) {
			status.IsFailed = true
		}

		if strings.HasPrefix(line, StatusDeployed) {
			status.IsDeployed = true
		}

		if len(line) == 0 {
			lastResource = ""

			columnDesired = -1
			columnCurrent = -1
			columnAvailable = -1

			columnName = -1
			columnType = -1
			columnClusterIP = -1
			columnExternalIP = -1
			columnPort = -1
		}

		if strings.HasPrefix(line, ResourcePrefix) {
			lastResource = strings.TrimPrefix(line, ResourcePrefix)
		}

		// namespace
		if strings.HasPrefix(line, NamespacePrefix) {
			status.Namespace = strings.TrimPrefix(line, NamespacePrefix)
		}

		// deployment time
		if strings.HasPrefix(line, DeploymentTimePrefix) {
			loc, _ := time.LoadLocation("Local")
			lastDeploymentTime, _ = time.ParseInLocation(time.ANSIC, strings.TrimPrefix(line, DeploymentTimePrefix), loc)
			status.DeploymentTime = lastDeploymentTime
		}

		// deployment columns
		indexDesired := strings.Index(line, DesiredLabel)
		indexCurrent := strings.Index(line, CurrentLabel)
		indexAvailable := strings.Index(line, AvailableLabel)

		if indexDesired >= 0 && indexCurrent >= 0 {
			columnDesired = indexDesired
			columnCurrent = indexCurrent

			if indexAvailable >= 0 {
				columnAvailable = indexAvailable
			}
		} else {
			if columnDesired >= 0 && columnCurrent >= 0 {
				nodesDesired := 0
				nodesAvailable := 0

				desired, desiredErr := strconv.Atoi(strings.Fields(line[columnDesired:])[0])
				current, currentErr := strconv.Atoi(strings.Fields(line[columnCurrent:])[0])

				if desiredErr == nil {
					nodesDesired = desired
				}

				if currentErr == nil {
					nodesAvailable = current
				}

				if columnAvailable >= 0 {
					available, availableErr := strconv.Atoi(strings.Fields(line[columnAvailable:])[0])

					if availableErr == nil {
						nodesAvailable = available
					}
				}

				status.DesiredNodes += nodesDesired
				status.AvailableNodes += nodesAvailable
			}
		}

		// service columns
		indexName := strings.Index(line, NameLabel)
		indexType := strings.Index(line, TypeLabel)
		indexClusterIP := strings.Index(line, ClusterIPLabel)
		indexExternalIP := strings.Index(line, ExternalIPLabel)
		indexPort := strings.Index(line, PortsLabel)

		if indexName >= 0 && indexType >= 0 && indexClusterIP >= 0 && indexExternalIP >= 0 && indexPort >= 0 {
			columnName = indexName
			columnType = indexType
			columnClusterIP = indexClusterIP
			columnExternalIP = indexExternalIP
			columnPort = indexPort
		} else {
			if columnName >= 0 && columnType >= 0 && columnClusterIP >= 0 && columnExternalIP >= 0 && columnPort >= 0 {
				svcName := strings.Fields(line[columnName:])[0]
				svcName = strings.TrimPrefix(svcName, release+"-")
				svcType := strings.Fields(line[columnType:])[0]

				status.Services[svcName] = &Service{
					Type:         svcType,
					NodePorts:    make(map[int]int),
					ClusterPorts: make(map[int]int),
				}

				// parse cluster ip
				clusterIP := strings.Fields(line[columnClusterIP:])[0]
				if clusterIP != "<none>" && clusterIP != "None" {
					status.Services[svcName].ClusterIP = clusterIP
				}

				// parse external ip
				externalIP := strings.Fields(line[columnExternalIP:])[0]
				if svcType == "LoadBalancer" {
					if externalIP == "<pending>" {
						status.PendingServices++
					} else if externalIP != "<none>" {
						status.Services[svcName].ExternalIP = externalIP
					}
				}

				// parse ports
				for _, portPair := range strings.Split(strings.Fields(line[columnPort:])[0], ",") {
					portFields := strings.FieldsFunc(portPair, func(c rune) bool {
						return c == ':' || c == '/'
					})

					if len(portFields) == 2 {
						clusterPort, clusterPortErr := strconv.Atoi(portFields[0])

						if clusterPortErr == nil {
							status.Services[svcName].ClusterPorts[clusterPort] = clusterPort
						}
					}

					if len(portFields) == 3 {
						nodePort, nodePortErr := strconv.Atoi(portFields[1])
						clusterPort, clusterPortErr := strconv.Atoi(portFields[0])

						if nodePortErr == nil && clusterPortErr == nil {
							status.Services[svcName].NodePorts[clusterPort] = nodePort
							status.Services[svcName].ClusterPorts[clusterPort] = clusterPort
						}
					}
				}
			}
		}

		_ = lastResource
	}

	return status, err
}

func IsReady() error {
	cmd := exec.Command("helm", "list", "--short")
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	err := cmd.Run()
	if _, exited := err.(*exec.ExitError); exited {
		msg := strings.TrimSpace(stderr.String())
		err = errors.New(msg)
	}
	return err
}

func RepoAdd(name string, repoURI string) error {
	uri, err := url.Parse(repoURI)
	if err != nil {
		return err
	}

	// extract and remove username and password form uri
	username := ""
	password := ""
	if uri.User != nil {
		username = uri.User.Username()
		password, _ = uri.User.Password()
		uri.User = nil
	}

	args := []string{"repo", "add", name, uri.String()}
	if len(username) > 0 {
		args = append(args, "--username", username)
	}

	if len(password) > 0 {
		args = append(args, "--password", password)
	}

	cmd := exec.Command("helm", args...)
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	err = cmd.Run()
	if _, exited := err.(*exec.ExitError); exited {
		msg := strings.TrimSpace(stderr.String())
		return errors.New(msg)
	}

	return nil
}
func RepoUpdate() error {
	cmd := exec.Command("helm", "repo", "update")
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	err := cmd.Run()
	if _, exited := err.(*exec.ExitError); exited {
		msg := strings.TrimSpace(stderr.String())
		err = errors.New(msg)
	}
	return err
}
