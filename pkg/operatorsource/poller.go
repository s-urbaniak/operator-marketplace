package operatorsource

import (
	"fmt"

	"github.com/operator-framework/operator-marketplace/pkg/appregistry"
	wrapper "github.com/operator-framework/operator-marketplace/pkg/client"
	"github.com/operator-framework/operator-marketplace/pkg/datastore"
	"github.com/operator-framework/operator-marketplace/pkg/phase"
	log "github.com/sirupsen/logrus"
)

// NewPoller returns a new instance of poller.
func NewPoller(client wrapper.Client) Poller {
	poller := &poller{
		datastore: datastore.Cache,
		helper: &pollHelper{
			factory:      appregistry.NewClientFactory(),
			datastore:    datastore.Cache,
			client:       client,
			transitioner: phase.NewTransitioner(),
		},
	}
	return poller
}

// Poller is an interface that wraps the Poll method.
//
// Poll iterates through all available operator source(s) that are in the
// underlying datastore and performs the following action(s):
//   a) It polls the remote registry namespace to check if there are any
//      update(s) available.
//
//   b) If there is an update available then it triggers a purge and rebuild
//      operation for the specified OperatorSource object.
//
// On any error during each iteration it logs the error encountered and moves
// on to the next OperatorSource object.
type Poller interface {
	Poll()
}

// poller implements the Poller interface.
type poller struct {
	helper    PollHelper
	datastore datastore.Writer
}

func (p *poller) Poll() {
	sources := p.datastore.GetAllOperatorSources()

	aggregators := []*datastore.PackageUpdateAggregator{}

	for _, source := range sources {
		aggregator := datastore.NewPackageUpdateAggregator(source.Name.Name)
		result, err := p.helper.HasUpdate(source)
		if err != nil {
			log.Errorf("[sync] error checking for updates [%s] - %v", source.Name, err)
			continue
		}

		if !result.RegistryHasUpdate {
			continue
		}

		log.Infof("operator source[%s] has updates: %s", source.Name, result)
		aggregator.Add(result)

		if err := p.trigger(source, result); err != nil {
			log.Errorf("%v", err)
		}

		// We have a list of operator(s) that have either been removed or have new
		// version(s). We should kick off CatalogSourceConfig reconciliation.
		if aggregator.IsUpdatedOrRemoved() {
			aggregators = append(aggregators, aggregator)
		}
	}
}

func (p *poller) trigger(source *datastore.OperatorSourceKey, result *datastore.UpdateResult) error {
	log.Infof("[sync] remote registry has update(s) - purging OperatorSource [%s]", source.Name)
	deleted, err := p.helper.TriggerPurge(source)
	if err != nil {
		return fmt.Errorf("[sync] error updating object [%s] - %v", source.Name, err)
	}

	if deleted {
		log.Infof("[sync] object deleted [%s] - no action taken", source.Name)
	}

	return nil
}
