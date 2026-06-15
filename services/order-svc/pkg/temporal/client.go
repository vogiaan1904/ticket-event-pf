package temporal

import (
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/config"
	"go.temporal.io/sdk/client"
)

// NewClient creates a new Temporal client
func NewClient(cfg config.TemporalConfig) (client.Client, error) {
	c, err := client.Dial(client.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Temporal client: %w", err)
	}

	return c, nil
}
