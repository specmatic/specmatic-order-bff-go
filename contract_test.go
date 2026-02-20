package main_test

import (
	"context"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go/network"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/config"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/test"
)

var authToken = "API-TOKEN-SPEC"

func TestContract(t *testing.T) {
	env := setUpEnv(t)
	defer tearDown(t, env)

	setUp(t, env)

	runTests(t, env)
}

func setUpEnv(t *testing.T) *test.TestEnvironment {
	config, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// create a context
	ctx := context.Background()

	// Create a net and store in env.
	newNetwork, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return &test.TestEnvironment{
		Ctx:                  ctx,
		Config:               config,
		BffTestNetwork:       newNetwork,
		ExpectedMessageCount: 2,
	}
}

func setUp(t *testing.T, env *test.TestEnvironment) {
	var err error

	printHeader(t, 1, "Starting Specmatic Mock (HTTP Stub + Kafka Mock)")
	env.SpecmaticMockContainer, err = test.StartSpecmaticMock(t, env)
	if err != nil {
		t.Fatalf("could not start specmatic mock container: %v", err)
	}

	printHeader(t, 2, "Starting BFF Service")
	env.BffServiceContainer, env.BffServiceDynamicPort, err = test.StartBFFService(t, env)
	if err != nil {
		t.Fatalf("could not start bff service container: %v", err)
	}
}

func runTests(t *testing.T, env *test.TestEnvironment) {
	printHeader(t, 3, "Starting tests")
	testLogs, err := test.RunTestContainer(env)

	if (err != nil) && !strings.Contains(err.Error(), "code 0") {
		t.Logf("Could not run test container: %s", err)
		t.Fail()
	}

	// Print test outcomes
	t.Log("Test Results:")
	t.Log(testLogs)
}

func tearDown(t *testing.T, env *test.TestEnvironment) {
	if env.BffServiceContainer != nil {
		if err := env.BffServiceContainer.Terminate(env.Ctx); err != nil {
			t.Logf("Failed to terminate BFF container: %v", err)
		}
	}

	if env.SpecmaticMockContainer != nil {
		t.Log("Verifying Kafka expectations...")
		err := test.VerifyKafkaExpectations(env)
		if err != nil {
			t.Logf("Kafka expectations were not met: %s", err)
			t.Fail()
		} else {
			t.Log("Kafka expectations met successfully")
		}
		test.StopSpecmaticMock(t, env)
	}
}

func printHeader(t *testing.T, stepNum int, title string) {
	t.Log("")
	t.Logf("======== STEP %d =========", stepNum)
	t.Log(title)
	t.Log("=========================")
	t.Log("")
}
