package release

import (
	"errors"
	"github.com/monostream/helmi/pkg/catalog"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strings"
)

var ReleaseNotFoundError = errors.New("release not found")

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

func Install(catalog *catalog.Catalog, serviceId string, planId string, id string, namespace string, acceptsIncomplete bool, parameters map[string]interface{}) error {
	name := getName(id)
	logger := getLogger()

	service := catalog.Service(serviceId)
	plan := service.Plan(planId)

	chart, chartErr := getChart(service, plan)
	chartVersion, chartVersionErr := getChartVersion(service, plan)
	chartValues, valuesErr := service.ChartValues(plan, name, parameters)

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

	err := helm.Install(name, chart, chartVersion, chartValues, namespace, acceptsIncomplete)

	if err != nil {
		logger.Error("failed to install release",
			zap.String("id", id),
			zap.String("name", name),
			zap.String("chart", chart),
			zap.String("chart-version", chartVersion),
			zap.String("serviceId", serviceId),
			zap.String("planId", planId),
			zap.String("namespace", namespace),
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
		zap.String("namespace", namespace))

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

			return ReleaseNotFoundError
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

			return Status{}, ReleaseNotFoundError
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
		IsAvailable: status.IsAvailable(),
	}, nil
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

			return nil, ReleaseNotFoundError
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

	credentials, err := service.UserCredentials(plan, nodes, status, values)
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
