package release

import (
	"errors"
	"regexp"
	"strings"
	"strconv"
	"go.uber.org/zap"
	"github.com/satori/go.uuid"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"github.com/monostream/helmi/pkg/catalog"
	"go.uber.org/zap/zapcore"
	"os"
	"reflect"
)

const lookupRegex = `\{\{\s*lookup\s*\(\s*'(?P<type>[\w]+)'\s*,\s*'(?P<path>[\w/:.]+)'\s*\)\s*\}\}`
const lookupRegexType = "type"
const lookupRegexPath = "path"

const lookupValue    = "value"
const lookupCluster  = "cluster"
const lookupUsername = "username"
const lookupPassword = "password"
const lookupEnv      = "env"
const lookupRelease  = "release"

type Status struct {
	IsFailed    bool
	IsDeployed  bool
	IsAvailable bool
}

func getLogger() *zap.Logger {
	//config := zap.NewProductionConfig()

	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.DisableCaller = true
	logger, _ := config.Build()

	return logger
}

func Install(catalog *catalog.Catalog, serviceId string, planId string, id string, acceptsIncomplete bool) error {
	name := getName(id)
	logger := getLogger()

	service, _ := catalog.GetService(serviceId)
	plan, _ := catalog.GetServicePlan(serviceId, planId)

	chart, chartErr := getChart(service, plan)
	chartVersion, chartVersionErr := getChartVersion(service, plan)
	chartValues := getChartValues(service, plan)

	if chartErr != nil {
		logger.Error("failed to install release",
			zap.String("id", id),
			zap.String("name", name),
			zap.String("serviceId", serviceId),
			zap.String("planId", planId),
			zap.Error(chartErr))

		return chartErr
	}

	if chartVersionErr != nil {
		chartVersion = ""
	}

	err := helm.Install(name, chart, chartVersion, chartValues, acceptsIncomplete)

	if err != nil {
		logger.Error("failed to install release",
			zap.String("id", id),
			zap.String("name", name),
			zap.String("chart", chart),
			zap.String("chart-version", chartVersion),
			zap.String("serviceId", serviceId),
			zap.String("planId", planId),
			zap.Error(err))

		return err
	}

	logger.Info("new release installed",
		zap.String("id", id),
		zap.String("name", name),
		zap.String("chart", chart),
		zap.String("chart-version", chartVersion),
		zap.String("serviceId", serviceId),
		zap.String("planId", planId))

	return nil
}

func Exists(id string) (bool, error) {
	name := getName(id)
	logger := getLogger()

	exists, err := helm.Exists(name)

	if err != nil {
		logger.Error("failed to check if release exists",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))
	}

	return exists, err
}

func Delete(id string) error {
	name := getName(id)
	logger := getLogger()

	err := helm.Delete(name)

	if err != nil {
		exists, existsErr := helm.Exists(name)

		if existsErr == nil && !exists {
			logger.Info("release deleted (not existed)",
				zap.String("id", id),
				zap.String("name", name))

			return nil
		}

		logger.Error("failed to delete release",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return err
	}

	logger.Info("release deleted",
		zap.String("id", id),
		zap.String("name", name))

	return nil
}

func GetStatus(id string) (Status, error) {
	name := getName(id)
	logger := getLogger()

	status, err := helm.GetStatus(name)

	if err != nil {
		exists, existsErr := helm.Exists(name)

		if existsErr == nil && !exists {
			logger.Info("asked status for deleted release",
				zap.String("id", id),
				zap.String("name", name))

			return Status{}, err
		}

		logger.Error("failed to get release status",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return Status{}, err
	}

	logger.Debug("sending release status",
		zap.String("id", id),
		zap.String("name", name))

	return Status{
		IsFailed:    status.IsFailed,
		IsDeployed:  status.IsDeployed,
		IsAvailable: status.AvailableNodes >= status.DesiredNodes,
	}, nil
}

func GetCredentials(catalog *catalog.Catalog, serviceId string, planId string, id string) (map[string]interface{}, error) {
	name := getName(id)
	logger := getLogger()

	service, _ := catalog.GetService(serviceId)
	plan, _ := catalog.GetServicePlan(serviceId, planId)

	status, err := helm.GetStatus(name)

	if err != nil {
		exists, existsErr := helm.Exists(name)

		if existsErr == nil && !exists {
			logger.Info("asked credentials for deleted release",
				zap.String("id", id),
				zap.String("name", name))

			return nil, err
		}

		logger.Error("failed to get release status",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return nil, err
	}

	nodes, err := kubectl.GetNodes()

	if err != nil {
		logger.Error("failed to get kubernetes nodes",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return nil, err
	}

	values, err := helm.GetValues(name)

	if err != nil {
		logger.Error("failed to get helm values",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return nil, err
	}

	credentials := getUserCredentials(service, plan, nodes, status, values)

	logger.Debug("sending release credentials",
		zap.String("id", id),
		zap.String("name", name))

	return credentials, nil
}

func getName(value string) string {
	const prefix = "helmi"

	if strings.HasPrefix(value, prefix) {
		return value
	}

	name := strings.ToLower(value)
	name = strings.Replace(name, "-", "", -1)
	name = strings.Replace(name, "_", "", -1)

	return prefix + name[:14]
}

func getChart(service catalog.CatalogService, plan catalog.CatalogPlan) (string, error) {
	if len(plan.Chart) > 0 {
		return plan.Chart, nil
	}

	if len(service.Chart) > 0 {
		return service.Chart, nil
	}

	return "", errors.New("no helm chart specified")
}

func getChartVersion(service catalog.CatalogService, plan catalog.CatalogPlan) (string, error) {
	if len(plan.ChartVersion) > 0 {
		return plan.ChartVersion, nil
	}

	if len(service.ChartVersion) > 0 {
		return service.ChartVersion, nil
	}

	return "", errors.New("no helm chart version specified")
}

func getChartValues(service catalog.CatalogService, plan catalog.CatalogPlan) map[string]string {
	values := map[string]string{}
	templates := map[string]string{}

	for key, value := range service.ChartValues {
		templates[key] = value
	}

	for key, value := range plan.ChartValues {
		templates[key] = value
	}

	usernames := map[string]string{}
	passwords := map[string]string{}

	r := regexp.MustCompile(lookupRegex)
	groupNames := r.SubexpNames()

	for key, template := range templates {
		value := r.ReplaceAllStringFunc(template, func(m string) string {
			var lookupType string
			var lookupPath string

			for groupKey, groupValue := range r.FindStringSubmatch(m) {
				groupName := groupNames[groupKey]

				if strings.EqualFold(groupName, lookupRegexType) {
					lookupType = groupValue
				}

				if strings.EqualFold(groupName, lookupRegexPath) {
					lookupPath = groupValue
				}
			}

			if strings.EqualFold(lookupType, lookupUsername) {
				username := usernames[lookupPath]

				if len(username) == 0 {
					username = uuid.NewV4().String()
					username = strings.Replace(username, "-", "", -1)
					usernames[lookupPath] = username
				}

				return username
			}

			if strings.EqualFold(lookupType, lookupPassword) {
				password := passwords[lookupPath]

				if len(password) == 0 {
					password = uuid.NewV4().String()
					password = strings.Replace(password, "-", "", -1)
					passwords[lookupPath] = password
				}

				return password
			}

			if strings.EqualFold(lookupType, lookupEnv) {
				env, _ := os.LookupEnv(lookupPath)
				return env
			}

			return ""
		})

		if len(value) > 0 {
			values[key] = value
		}
	}

	return values
}

func getUserCredentials(service catalog.CatalogService, plan catalog.CatalogPlan, kubernetesNodes [] kubectl.Node, helmStatus helm.Status, helmValues map[string]string) map[string]interface{} {
	values := map[string]interface{}{}
	templates := map[string]interface{}{}

	for key, value := range service.UserCredentials {
		templates[key] = value
	}

	for key, value := range plan.UserCredentials {
		templates[key] = value
	}

	r := regexp.MustCompile(lookupRegex)
	groupNames := r.SubexpNames()

	replaceTemplate := func(template string) string {
		var lookupType string
		var lookupPath string

		for groupKey, groupValue := range r.FindStringSubmatch(template) {
			groupName := groupNames[groupKey]

			if strings.EqualFold(groupName, lookupRegexType) {
				lookupType = groupValue
			}

			if strings.EqualFold(groupName, lookupRegexPath) {
				lookupPath = groupValue
			}
		}

		if strings.EqualFold(lookupType, lookupRelease) {
			if strings.EqualFold(lookupPath, "name") {
				return helmStatus.Name
			}
			if strings.EqualFold(lookupPath, "namespace") {
				return helmStatus.Namespace
			}
		}

		if strings.EqualFold(lookupType, lookupUsername) {
			username := helmValues[lookupPath]
			return username
		}

		if strings.EqualFold(lookupType, lookupPassword) {
			password := helmValues[lookupPath]
			return password
		}

		if strings.EqualFold(lookupType, lookupValue) {
			value := helmValues[lookupPath]
			return value
		}

		if strings.EqualFold(lookupType, lookupCluster) {
			if strings.HasPrefix(strings.ToLower(lookupPath), "port") {
				portParts := strings.Split(lookupPath, ":")

				for clusterPort, nodePort := range helmStatus.NodePorts {
					if len(portParts) == 1 || strings.EqualFold(strconv.Itoa(clusterPort), portParts[1]) {
						return strconv.Itoa(nodePort)
					}
				}

				for clusterPort, nodePort := range helmStatus.ClusterPorts {
					if len(portParts) == 1 || strings.EqualFold(strconv.Itoa(clusterPort), portParts[1]) {
						return strconv.Itoa(nodePort)
					}
				}

				if len(portParts) > 0 {
					return portParts[0]
				}

				return "0"
			}

			// single host

			if strings.EqualFold(lookupPath, "address") {
				// return dns name if set as environment variable
				if value, ok := os.LookupEnv("DOMAIN"); ok {
					return value
				}

				for _, node := range kubernetesNodes {
					if len(node.ExternalIP) > 0 {
						return node.ExternalIP
					}
				}

				for _, node := range kubernetesNodes {
					if len(node.InternalIP) > 0 {
						return node.InternalIP
					}
				}
			}

			if strings.EqualFold(lookupPath, "hostname") {
				for _, node := range kubernetesNodes {
					if len(node.Hostname) > 0 {
						return node.Hostname
					}
				}
			}
		}

		return ""
	}

	for key, templateInterface := range templates {
		// string
		templateString, ok := reflect.ValueOf(templateInterface).Interface().(string)

		if ok {
			value := r.ReplaceAllStringFunc(templateString, replaceTemplate)

			if len(value) > 0 {
				values[key] = value
			}

			continue
		}

		// string array
		templateStringArray, ok := reflect.ValueOf(templateInterface).Interface().([]interface{})

		if ok {
			valueArray := []string{}

			for _, templateValue := range templateStringArray {
				templateString, ok := reflect.ValueOf(templateValue).Interface().(string)

				if ok {
					value := r.ReplaceAllStringFunc(templateString, replaceTemplate)

					if len(value) > 0 {
						valueArray = append(valueArray, value)
					}
				}
			}

			if len(valueArray) > 0 {
				values[key] = valueArray
			}

			continue
		}
	}

	return values
}
