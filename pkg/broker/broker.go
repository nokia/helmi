package broker

import (
	"context"

	"github.com/monostream/helmi/pkg/catalog"
	"github.com/pivotal-cf/brokerapi"
)

type Broker struct {
	Catalog catalog.Catalog
}

func (b *Broker) Initialize(path string) {
	b.Catalog.Parse(path)
}

func (b *Broker) Services() []brokerapi.Service {
	services := make([]brokerapi.Service, 0, len(b.Catalog.Services))

	for _, service := range b.Catalog.Services {
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

	return services
}

func (b *Broker) Provision(ctx context.Context, instanceID string, details brokerapi.ProvisionDetails, asyncAllowed bool) (spec brokerapi.ProvisionedServiceSpec, err error) {
	spec = brokerapi.ProvisionedServiceSpec{}

	return spec, nil
}
