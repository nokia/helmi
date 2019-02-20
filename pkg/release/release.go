package release

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/monostream/helmi/pkg/catalog"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var ErrReleaseNotFound = errors.New("release not found")

type Health struct {
	IsFailed       bool
	IsReady        bool
	deploymentTime time.Time
}

func (h *Health) IsTimedOut() bool {
	timeout, exists := os.LookupEnv("TIMEOUT")
	if !exists {
		timeout = "30m"
	}
	duration, _ := time.ParseDuration(timeout)
	if time.Now().After(h.deploymentTime.Add(duration)) && !h.IsReady {
		return true
	}

	return false
}

func getLogger() *zap.Logger {
	//config := zap.NewProductionConfig()

	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.DisableCaller = true
	logger, _ := config.Build()

	return logger
}

func Install(catalog *catalog.Catalog, serviceId string, planId string, id string, namespace kubectl.Namespace, acceptsIncomplete bool, parameters map[string]interface{}, contextValues map[string]interface{}) error {
	name := getName(id)
	logger := getLogger()

	service := catalog.Service(serviceId)
	plan := service.Plan(planId)

	chart, chartErr := getChart(service, plan)
	chartVersion, chartVersionErr := getChartVersion(service, plan)
	chartValues, valuesErr := service.ChartValues(plan, name, namespace, parameters, contextValues)

	if chartErr != nil {
		logger.Error("failed to read chart from catalog definition",
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

	if valuesErr != nil {
		logger.Error("failed to parse chart-values section",
			zap.String("id", id),
			zap.String("name", name),
			zap.String("serviceId", serviceId),
			zap.String("planId", planId),
			zap.Error(valuesErr))

		return valuesErr
	}

	err := helm.Install(name, chart, chartVersion, chartValues, namespace.Name, acceptsIncomplete)

	if err != nil {
		logger.Error("failed to install release",
			zap.String("id", id),
			zap.String("name", name),
			zap.String("chart", chart),
			zap.String("chart-version", chartVersion),
			zap.String("serviceId", serviceId),
			zap.String("planId", planId),
			zap.String("namespace", namespace.Name),
			zap.Error(err))

		return err
	}

	logger.Info("new release installed",
		zap.String("id", id),
		zap.String("name", name),
		zap.String("chart", chart),
		zap.String("chart-version", chartVersion),
		zap.String("serviceId", serviceId),
		zap.String("planId", planId),
		zap.String("namespace", namespace.Name))

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

			return ErrReleaseNotFound
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

func GetHealth(c *catalog.Catalog, id string) (Health, error) {
	name := getName(id)
	logger := getLogger()

	status, err := helm.GetStatus(name)
	if err != nil {
		exists, existsErr := helm.Exists(name)
		if existsErr == nil && !exists {
			logger.Info("asked status for deleted release",
				zap.String("id", id),
				zap.String("name", name))

			return Health{}, ErrReleaseNotFound
		}

		logger.Error("failed to get release status",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return Health{}, err
	}

	health := Health{
		IsFailed:       status.IsFailed,
		IsReady:        false,
		deploymentTime: status.DeploymentTime,
	}

	if !status.IsAvailable() {
		return health, nil
	}

	values, err := helm.GetValues(name)
	if err != nil {
		logger.Error("failed to get helm values",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return Health{}, err
	}

	metadata, err := catalog.ExtractMetadata(values)
	if err != nil {
		logger.Error("failed to fetch helmi metadata",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return Health{}, err
	}

	service := c.Service(metadata.ServiceId)
	plan := service.Plan(metadata.PlanId)

	nodes, err := kubectl.GetNodes()
	if err != nil {
		logger.Error("failed to get kubernetes nodes",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return Health{}, err
	}

	release, err := service.ReleaseSection(plan, nodes, status, values)
	if err != nil {
		logger.Error("failed to parse user credentials",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return Health{}, err
	}

	checkFailed := false
	for _, healthCheckURL := range release.HealthCheckURLs {
		err = checkHealth(healthCheckURL)
		if err != nil {
			logger.Info("health check failed",
				zap.String("id", id),
				zap.String("name", name),
				zap.String("url", healthCheckURL),
				zap.Error(err),
			)
			checkFailed = true
			break
		}
	}

	health.IsReady = !checkFailed
	return health, nil
}

func GetCredentials(catalog *catalog.Catalog, serviceId string, planId string, id string) (map[string]interface{}, error) {
	name := getName(id)
	logger := getLogger()

	service := catalog.Service(serviceId)
	plan := service.Plan(planId)

	status, err := helm.GetStatus(name)
	if err != nil {
		exists, existsErr := helm.Exists(name)

		if existsErr == nil && !exists {
			logger.Info("asked credentials for deleted release",
				zap.String("id", id),
				zap.String("name", name))

			return nil, ErrReleaseNotFound
		}

		logger.Error("failed to get release status",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return nil, err
	}

	if !status.IsAvailable() {
		return nil, errors.New("service not yet available")
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

	release, err := service.ReleaseSection(plan, nodes, status, values)
	if err != nil {
		logger.Error("failed to parse user credentials",
			zap.String("id", id),
			zap.String("name", name),
			zap.Error(err))

		return nil, err
	}

	logger.Debug("sending release credentials",
		zap.String("id", id),
		zap.String("name", name))

	return release.UserCredentials, nil
}

func min(x int, y int) int {
	if x > y {
		return y
	}
	return x
}

func getName(value string) string {
	const prefix = "helmi"
	const maxLengthNoPrefix = 14

	if strings.HasPrefix(value, prefix) {
		return value
	}

	name := strings.ToLower(value)
	name = strings.Replace(name, "-", "", -1)
	name = strings.Replace(name, "_", "", -1)

	return prefix + name[:min(maxLengthNoPrefix, len(name))]
}

func getChart(service *catalog.Service, plan *catalog.Plan) (string, error) {
	if len(plan.Chart) > 0 {
		return plan.Chart, nil
	}

	if len(service.Chart) > 0 {
		return service.Chart, nil
	}

	return "", errors.New("no helm chart specified")
}

func getChartVersion(service *catalog.Service, plan *catalog.Plan) (string, error) {
	if len(plan.ChartVersion) > 0 {
		return plan.ChartVersion, nil
	}

	if len(service.ChartVersion) > 0 {
		return service.ChartVersion, nil
	}

	return "", errors.New("no helm chart version specified")
}

const healthCheckTimeout = time.Second * 10

func checkHealth(endpoint string) error {
	info, err := url.Parse(endpoint)
	if err != nil {
		return err
	}

	switch info.Scheme {
	case "http":
		return checkHealthHTTP(endpoint)
	case "https":
		return checkHealthHTTP(endpoint)
	case "tcp":
		return checkHealthTCP(info.Host)
	case "tls":
		return checkHealthTLS(info.Host)
	default:
		return fmt.Errorf("unsupported url scheme: %s", info.Scheme)
	}
}

func checkHealthHTTP(url string) error {
	client := &http.Client{
		Timeout: healthCheckTimeout,
	}
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode > 399 {
		return fmt.Errorf("health check returned http status %d", res.StatusCode)
	}

	return nil
}

func checkHealthTCP(address string) error {
	conn, err := net.DialTimeout("tcp", address, healthCheckTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

func checkHealthTLS(address string) error {
	config := &tls.Config{
		// use host CAs
		RootCAs: nil,
	}
	dialer := &net.Dialer{
		Timeout: healthCheckTimeout,
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", address, config)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Handshake()
}
