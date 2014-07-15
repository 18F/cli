package serviceplan

import (
	"fmt"

	"github.com/cloudfoundry/cli/cf/api"
	"github.com/cloudfoundry/cli/cf/command_metadata"
	"github.com/cloudfoundry/cli/cf/configuration"
	"github.com/cloudfoundry/cli/cf/models"
	"github.com/cloudfoundry/cli/cf/requirements"
	"github.com/cloudfoundry/cli/cf/terminal"
	"github.com/codegangsta/cli"
)

type ServiceAccess struct {
	ui         terminal.UI
	config     configuration.Reader
	brokerRepo api.ServiceBrokerRepository
}

func NewServiceAccess(ui terminal.UI, config configuration.Reader, brokerRepo api.ServiceBrokerRepository) (cmd *ServiceAccess) {
	return &ServiceAccess{
		ui:         ui,
		config:     config,
		brokerRepo: brokerRepo,
	}
}

func (cmd *ServiceAccess) Metadata() command_metadata.CommandMetadata {
	return command_metadata.CommandMetadata{
		Name:        "service-access",
		Description: "List service access settings",
		Usage:       "CF_NAME service-access",
	}
}

func (cmd *ServiceAccess) GetRequirements(requirementsFactory requirements.Factory, c *cli.Context) (reqs []requirements.Requirement, err error) {
	reqs = []requirements.Requirement{
		requirementsFactory.NewLoginRequirement(),
	}
	return
}

func (cmd *ServiceAccess) Run(c *cli.Context) {
	brokers := []models.ServiceBroker{}
	apiErr := cmd.brokerRepo.ListServiceBrokers(func(serviceBroker models.ServiceBroker) bool {
		brokers = append(brokers, serviceBroker)
		return true
	})

	if apiErr != nil {
		cmd.ui.Failed("Failed fetching service brokers.\n%s", apiErr)
		return
	}

	for _, serviceBroker := range brokers {
		cmd.ui.Say(fmt.Sprintf("broker: %s", serviceBroker.Name))
	}
}
