package broker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"code.cloudfoundry.org/lager"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/pivotal-cf/brokerapi"

	"encoding/json"
	"github.com/monostream/helmi/pkg/catalog"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/release"
)

type Config struct {
	Username string
	Password string
	Address  string
}

type Broker struct {
	catalog catalog.Catalog
	logger  lager.Logger
	router  *mux.Router
	addr    string
}

func NewBroker(catalog catalog.Catalog, config Config, logger lager.Logger) *Broker {
	router := mux.NewRouter()
	b := &Broker{
		catalog: catalog,
		logger:  logger,
		router:  router,
		addr:    config.Address,
	}

	brokerapi.AttachRoutes(b.router, b, logger)
	liveness := b.router.HandleFunc("/liveness", b.livenessHandler).Methods(http.MethodGet)
	readiness := b.router.HandleFunc("/readiness", b.readinessHandler).Methods(http.MethodGet)

	// list of routes which do not require authentication
	noAuthRequired := skipAuth{
		liveness:  true,
		readiness: true,
	}

	b.router.Use(authHandler(config, noAuthRequired))
	b.router.Use(handlers.ProxyHeaders)
	b.router.Use(handlers.CompressHandler)
	b.router.Use(handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{http.MethodHead, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}),
		handlers.AllowCredentials(),
	))

	return b
}

func (b *Broker) Run() {
	log.Println("Helmi is ready and available on port " + strings.TrimPrefix(b.addr, ":"))
	log.Fatal(http.ListenAndServe(b.addr, b.router))
}

// if requests to this handler fail, Kubernetes will restart the container
func (b *Broker) livenessHandler(w http.ResponseWriter, r *http.Request) {
	b.writeJSONResponse(w, http.StatusOK, struct{}{})
}

func (b *Broker) readinessHandler(w http.ResponseWriter, r *http.Request) {
	err := helm.IsReady()
	if err != nil {
		b.writeJSONError(w, err)
		return
	}
	b.writeJSONResponse(w, http.StatusOK, struct{}{})
}

func (b *Broker) Services(ctx context.Context) ([]brokerapi.Service, error) {
	services := make([]brokerapi.Service, 0, len(b.catalog.Services))

	for _, service := range b.catalog.Services {
		servicePlans := make([]brokerapi.ServicePlan, 0, len(service.Plans))

		isFree := true
		isBindable := true

		for _, plan := range service.Plans {
			p := brokerapi.ServicePlan{
				ID:          plan.Id,
				Name:        plan.Name,
				Description: plan.Description,
				Free:        &isFree,
				Bindable:    &isBindable,
			}
			servicePlans = append(servicePlans, p)
		}

		s := brokerapi.Service{
			ID:            service.Id,
			Name:          service.Name,
			Description:   service.Description,
			Bindable:      true,
			PlanUpdatable: false,
			Plans:         servicePlans,
		}
		services = append(services, s)
	}

	return services, nil
}

func namespaceFromContext(raw json.RawMessage) string {
	if namespace, ok := os.LookupEnv("HELM_NAMESPACE"); ok {
		return namespace
	}

	var cfContext struct {
		Platform  string `json:"platform"`
		SpaceGUID string `json:"space_guid"`
		OrgGUID   string `json:"organization_guid"`
	}

	err := json.Unmarshal(raw, &cfContext)
	if err == nil && cfContext.Platform == "cloudfoundry" && len(cfContext.SpaceGUID) >= 8 {
		return fmt.Sprintf("cf-%s", cfContext.SpaceGUID[:8])
	}

	return ""
}

func (b *Broker) Provision(ctx context.Context, instanceID string, details brokerapi.ProvisionDetails, asyncAllowed bool) (brokerapi.ProvisionedServiceSpec, error) {
	spec := brokerapi.ProvisionedServiceSpec{}

	parameters := make(map[string]interface{})
	if details.RawParameters != nil {
		err := json.Unmarshal(details.RawParameters, &parameters)
		if err != nil {
			return spec, brokerapi.ErrRawParamsInvalid
		}
	}

	namespace := namespaceFromContext(details.RawContext)
	err := release.Install(&b.catalog, details.ServiceID, details.PlanID, instanceID, namespace, asyncAllowed, parameters)
	if err != nil {
		exists, existsErr := release.Exists(instanceID)

		if existsErr == nil && exists {
			return spec, brokerapi.ErrInstanceAlreadyExists
		}
	}

	spec.IsAsync = asyncAllowed
	return spec, err
}

func (b *Broker) Deprovision(ctx context.Context, instanceID string, details brokerapi.DeprovisionDetails, asyncAllowed bool) (brokerapi.DeprovisionServiceSpec, error) {
	spec := brokerapi.DeprovisionServiceSpec{}
	err := release.Delete(instanceID)
	if err == release.ErrReleaseNotFound {
		return spec, brokerapi.ErrInstanceDoesNotExist
	}
	return spec, err
}

func (b *Broker) Bind(ctx context.Context, instanceID, bindingID string, details brokerapi.BindDetails) (brokerapi.Binding, error) {
	binding := brokerapi.Binding{}
	credentials, err := release.GetCredentials(&b.catalog, details.ServiceID, details.PlanID, instanceID)

	if err != nil {
		if err == release.ErrReleaseNotFound {
			return binding, brokerapi.ErrInstanceDoesNotExist
		}

		return binding, err
	}

	binding.Credentials = credentials
	return binding, nil
}

func (b *Broker) Unbind(ctx context.Context, instanceID, bindingID string, details brokerapi.UnbindDetails) error {
	exists, err := release.Exists(instanceID)

	if err != nil {
		return err
	} else if !exists {
		return brokerapi.ErrBindingDoesNotExist
	} else {
		return nil
	}
}

func (b *Broker) LastOperation(ctx context.Context, instanceID, operationData string) (brokerapi.LastOperation, error) {
	op := brokerapi.LastOperation{}
	health, err := release.GetHealth(&b.catalog, instanceID)

	if err != nil {
		if err == release.ErrReleaseNotFound {
			return op, brokerapi.ErrInstanceDoesNotExist
		}

		return op, err
	}

	if health.IsFailed || health.IsTimedOut() {
		op.State = "failed"
	} else if health.IsReady {
		op.State = "succeeded"
	} else {
		op.State = "in progress"
	}

	return op, nil
}

func (b *Broker) Update(ctx context.Context, instanceID string, details brokerapi.UpdateDetails, asyncAllowed bool) (brokerapi.UpdateServiceSpec, error) {
	return brokerapi.UpdateServiceSpec{}, brokerapi.ErrPlanChangeNotSupported
}

type skipAuth map[*mux.Route]bool

func authHandler(config Config, noAuthRequired skipAuth) mux.MiddlewareFunc {
	validCredentials := func(r *http.Request) bool {
		// disable authentication if configuration variables not set
		if config.Username == "" || config.Password == "" {
			return true
		}
		// some routes do not require authentication
		if noAuthRequired[mux.CurrentRoute(r)] {
			return true
		}

		username, password, isOk := r.BasicAuth()
		if isOk && username == config.Username && password == config.Password {
			return true
		}

		return false
	}

	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validCredentials(r) {
				http.Error(w, "Unauthorized.", http.StatusUnauthorized)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

func (b *Broker) writeJSONResponse(w http.ResponseWriter, status int, response interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	err := encoder.Encode(response)
	if err != nil {
		b.logger.Error("encoding response", err, lager.Data{"status": status, "response": response})
	}
}

func (b *Broker) writeJSONError(w http.ResponseWriter, err error) {
	b.writeJSONResponse(w, http.StatusInternalServerError, brokerapi.ErrorResponse{
		Description: err.Error(),
	})
}
