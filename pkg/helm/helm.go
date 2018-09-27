package helm

import (
	"bufio"
	"bytes"
	"strings"
	"strconv"
	"os/exec"
	"gopkg.in/yaml.v2"
	"time"
	"os"
	"errors"
)

type Status struct {
	Name       string
	Namespace  string
	IsFailed   bool
	IsDeployed bool

	DesiredNodes int
	AvailableNodes int
	PendingServices int

	NodePorts map[int]int
	ClusterPorts map[int]int
	ExternalIPs []string
}

func (s *Status) IsAvailable() bool {
	return s.AvailableNodes >= s.DesiredNodes && s.PendingServices == 0
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

func Install(release string, chart string, version string, values map[string]interface{}, acceptsIncomplete bool) (error) {
	arguments := [] string{}

	arguments = append(arguments, "install", chart)
	arguments = append(arguments, "--name", release)

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
	output, err := cmd.CombinedOutput()
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
		DesiredNodes: 0,
		AvailableNodes: 0,

		NodePorts:    map[int]int{},
		ClusterPorts: map[int]int{},
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

	const TypeLabel = "TYPE"
	const ExternalIPLabel = "EXTERNAL-IP"
	const PortsLabel = "PORT(S)"


	var lastResource string
	var lastDeploymentTime time.Time

	columnDesired := -1
	columnCurrent := -1
	columnAvailable := -1

	columnType := -1
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

			columnType = -1
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
		}

		// deployment columns
		indexDesired := strings.Index(line, DesiredLabel)
		indexCurrent := strings.Index(line, CurrentLabel)
		indexAvailable := strings.Index(line, AvailableLabel)

		// service columns
		indexType := strings.Index(line, TypeLabel)
		indexExternalIP := strings.Index(line, ExternalIPLabel)
		indexPort := strings.Index(line, PortsLabel)


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

		if indexPort >= 0 {
			columnPort = indexPort
		} else {
			if columnPort >= 0 {
				for _, portPair := range strings.Split(strings.Fields(line[columnPort:])[0], ",") {
					portFields := strings.FieldsFunc(portPair, func(c rune) bool {
						return c == ':' || c == '/'
					})

					if len(portFields) == 2 {
						clusterPort, clusterPortErr := strconv.Atoi(portFields[0])

						if clusterPortErr == nil {
							status.ClusterPorts[clusterPort] = clusterPort
						}
					}

					if len(portFields) == 3 {
						nodePort, nodePortErr := strconv.Atoi(portFields[1])
						clusterPort, clusterPortErr := strconv.Atoi(portFields[0])

						if nodePortErr == nil && clusterPortErr == nil {
							status.NodePorts[clusterPort] = nodePort
							status.ClusterPorts[clusterPort] = clusterPort
						}
					}
				}
			}
		}

		if indexType >= 0 && indexExternalIP >= 0 {
			columnType = indexType
			columnExternalIP = indexExternalIP
		} else {
			if columnType >= 0 && columnExternalIP >= 0 {
				svcType := strings.Fields(line[columnType:])[0]
				externalIP := strings.Fields(line[columnExternalIP:])[0]

				if svcType == "LoadBalancer" {
					if externalIP == "<pending>" {
						status.PendingServices++
					} else if externalIP != "<none>" {
						status.ExternalIPs = append(status.ExternalIPs, externalIP)
					}
				}
			}
		}

		_ = lastResource
	}

	// timeout
	timeout, exists := os.LookupEnv("TIMEOUT")
	if !exists {
		timeout = "30m"
	}
	duration, _ := time.ParseDuration(timeout)
	if time.Now().After(lastDeploymentTime.Add(duration)) && status.AvailableNodes < status.DesiredNodes {
		status.IsFailed = true
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
