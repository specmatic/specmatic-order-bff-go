package test

import (
	"context"

	"github.com/testcontainers/testcontainers-go"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/config"
)

type TestEnvironment struct {
	Ctx                    context.Context
	BffTestNetwork         *testcontainers.DockerNetwork
	SpecmaticMockContainer testcontainers.Container
	KafkaDynamicAPIPort    string
	KafkaAPIHost           string
	KafkaExternalPort      string
	BffServiceContainer    testcontainers.Container
	BffServiceDynamicPort  string
	ExpectedMessageCount   int
	Config                 *config.Config
}
